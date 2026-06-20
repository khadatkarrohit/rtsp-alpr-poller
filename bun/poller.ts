// ─────────────────────────────────────────
// ALPR Poller — Bun (TypeScript)
// Camera: Prama PT-NC140D3-WNM(D2)
// ─────────────────────────────────────────
// Install: curl -fsSL https://bun.sh/install | bash
// Run:     bun run poller.ts

// ── Config (edit these)
const RTSP_URL    = 'rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101';
const API_KEY     = 'your key, if any';
const API_BASE    = 'your api url';
const COUNTRY     = 'IND';
const INTERVAL_MS = 2000;

const CURR_FRAME = '/tmp/alpr_curr.jpg';
const PREV_FRAME = '/tmp/alpr_prev.jpg';

const sleep = (ms: number) => new Promise(r => setTimeout(r, ms));

// ── Capture one JPEG frame via ffmpeg
async function captureFrame(): Promise<{ current: Buffer; previous: Buffer | null }> {
  const curr = Bun.file(CURR_FRAME);
  if (await curr.exists()) {
    await Bun.write(PREV_FRAME, await curr.arrayBuffer());
  }

  const proc = Bun.spawn([
    'ffmpeg', '-loglevel', 'error',
    '-rtsp_transport', 'tcp',
    '-i', RTSP_URL,
    '-frames:v', '1', '-q:v', '3',
    '-vf', 'scale=640:480',
    CURR_FRAME, '-y',
  ], { stdout: 'ignore', stderr: 'ignore' });

  const code = await proc.exited;
  if (code !== 0) throw new Error(`ffmpeg exited ${code}`);

  const current  = Buffer.from(await Bun.file(CURR_FRAME).arrayBuffer());
  const prevFile = Bun.file(PREV_FRAME);
  const previous = (await prevFile.exists())
    ? Buffer.from(await prevFile.arrayBuffer())
    : null;

  return { current, previous };
}

// ── Motion detection: MD5 hash check + sampled byte diff
async function detectMotion(current: Buffer, previous: Buffer | null): Promise<boolean> {
  if (!previous) return false;

  const h1 = Bun.CryptoHasher.hash('md5', current,  'hex');
  const h2 = Bun.CryptoHasher.hash('md5', previous, 'hex');
  if (h1 === h2) return false;

  const len = Math.min(current.length, previous.length);
  let diff = 0;
  for (let i = 0; i < len; i += 10) diff += Math.abs(current[i] - previous[i]);
  const score = (diff / (len / 10)) / 255 * 100;
  console.log(`[motion] ${score.toFixed(2)}%`);
  return score > 30;
}

// ── POST frame to ALPR API
async function scanPlate(): Promise<Record<string, unknown>> {
  const form = new FormData();
  form.append('image', new Blob([await Bun.file(CURR_FRAME).arrayBuffer()], { type: 'image/jpeg' }), 'frame.jpg');
  form.append('country', COUNTRY);

  const res = await fetch(`${API_BASE}/plate/recognize`, {
    method: 'POST',
    headers: { 'x-api-key': API_KEY },
    body: form,
  });
  if (!res.ok) throw new Error(`ALPR API ${res.status}`);
  return res.json();
}

// ── Notify backend after confirmed plate
async function notifyBackend(result: Record<string, unknown>): Promise<void> {
  await fetch(`${API_BASE.replace('/api/v1', '')}/api/entry`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'x-api-key': API_KEY },
    body: JSON.stringify({
      plate:      result.plate,
      confidence: result.confidence,
      country:    COUNTRY,
      timestamp:  new Date().toISOString(),
      source:     'bun-poller-v1',
    }),
  });
}

// ── Main loop
console.log('[alpr] bun poller started');
console.log(`[camera] ${RTSP_URL}`);

let lastPlateTime     = 0;
let consecutiveErrors = 0;

while (true) {
  const t0 = Date.now();
  try {
    if (Date.now() - lastPlateTime < 8000) {
      console.log('[cooldown] skipping');
      await sleep(INTERVAL_MS);
      continue;
    }

    const { current, previous } = await captureFrame();
    consecutiveErrors = 0;

    if (!await detectMotion(current, previous)) {
      console.log('[motion] none');
    } else {
      console.log('[motion] detected — scanning plate');
      const result = await scanPlate();
      console.log('[alpr]', JSON.stringify(result));

      if (result.plate) {
        console.log(`[plate] ${result.plate} (${result.confidence}%)`);
        await notifyBackend(result);
        lastPlateTime = Date.now();
        console.log('[cooldown] 8s');
      } else {
        console.log('[plate] none found');
      }
    }
  } catch (err: unknown) {
    consecutiveErrors++;
    console.error(`[error] #${consecutiveErrors}: ${(err as Error).message}`);
    if (consecutiveErrors >= 3) {
      console.error('[error] too many failures, waiting 30s');
      await sleep(30_000);
      consecutiveErrors = 0;
    }
  }

  const elapsed = Date.now() - t0;
  if (elapsed < INTERVAL_MS) await sleep(INTERVAL_MS - elapsed);
}
