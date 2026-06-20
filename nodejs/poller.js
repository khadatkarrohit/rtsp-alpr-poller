// ─────────────────────────────────────────
// ALPR Poller — Node.js  ★ OPTIMIZED
// Camera: Prama PT-NC140D3-WNM(D2)
// ─────────────────────────────────────────
// Single file, zero npm dependencies (requires Node.js >= 18).
// fetch and FormData are built-in from Node 18 — no npm install needed.
//
// Run: node --input-type=module poller.js
//  or: rename to poller.mjs then  node poller.mjs

import { execSync } from 'child_process';
import { existsSync, copyFileSync, readFileSync } from 'fs';
import { createHash } from 'crypto';

// ── Config (edit these)
const RTSP_URL         = 'rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101';
const API_KEY          = 'your key, if any';
const API_BASE         = 'your api url';
const COUNTRY          = 'IND';
const INTERVAL_MS      = 2000;
const COOLDOWN_MS      = 8000;
const MOTION_THRESHOLD = 30;
const MAX_RETRIES      = 3;

const CURR_FRAME = '/tmp/alpr_curr.jpg';
const PREV_FRAME = '/tmp/alpr_prev.jpg';

const sleep = ms => new Promise(r => setTimeout(r, ms));

// ── Capture one JPEG frame via ffmpeg
function captureFrame() {
  if (existsSync(CURR_FRAME)) copyFileSync(CURR_FRAME, PREV_FRAME);

  execSync(
    `ffmpeg -loglevel error -rtsp_transport tcp \
     -i "${RTSP_URL}" \
     -frames:v 1 -q:v 3 -vf scale=640:480 ${CURR_FRAME} -y`,
    { timeout: 6000 },
  );

  return {
    current:  readFileSync(CURR_FRAME),
    previous: existsSync(PREV_FRAME) ? readFileSync(PREV_FRAME) : null,
  };
}

// ── Motion detection: MD5 hash check + sampled byte diff
function detectMotion(current, previous) {
  if (!previous) return false;

  const h1 = createHash('md5').update(current).digest('hex');
  const h2 = createHash('md5').update(previous).digest('hex');
  if (h1 === h2) return false;

  const len = Math.min(current.length, previous.length);
  let diff = 0;
  for (let i = 0; i < len; i += 10) diff += Math.abs(current[i] - previous[i]);
  const score = (diff / (len / 10)) / 255 * 100;
  console.log(`[motion] ${score.toFixed(2)}%`);
  return score > MOTION_THRESHOLD;
}

// ── POST frame to ALPR API (uses built-in fetch + FormData from Node 18)
async function scanPlate() {
  const form = new FormData();
  form.append('image', new Blob([readFileSync(CURR_FRAME)], { type: 'image/jpeg' }), 'frame.jpg');
  form.append('country', COUNTRY);

  const res = await fetch(`${API_BASE}/plate/recognize`, {
    method:  'POST',
    headers: { 'x-api-key': API_KEY },
    body:    form,
  });
  if (!res.ok) throw new Error(`ALPR API ${res.status}`);
  return res.json();
}

// ── Notify backend after confirmed plate
async function notifyBackend(result) {
  await fetch(`${API_BASE}/entry`, {
    method:  'POST',
    headers: { 'Content-Type': 'application/json', 'x-api-key': API_KEY },
    body:    JSON.stringify({
      plate:      result.plate,
      confidence: result.confidence,
      country:    COUNTRY,
      timestamp:  new Date().toISOString(),
      source:     'node-poller-v3',
    }),
  });
}

// ── Main loop
async function main() {
  console.log('[alpr] node.js poller started');
  console.log(`[camera] ${RTSP_URL}`);

  let lastPlateTime     = 0;
  let consecutiveErrors = 0;

  while (true) {
    const t0 = Date.now();
    try {
      if (Date.now() - lastPlateTime < COOLDOWN_MS) {
        console.log('[cooldown] skipping');
        await sleep(INTERVAL_MS);
        continue;
      }

      const { current, previous } = captureFrame();
      consecutiveErrors = 0;

      if (!detectMotion(current, previous)) {
        console.log('[motion] none');
      } else {
        console.log('[motion] detected — scanning plate');
        const result = await scanPlate();
        console.log('[alpr]', JSON.stringify(result));

        if (result?.plate) {
          console.log(`[plate] ${result.plate} (${result.confidence}%)`);
          await notifyBackend(result);
          lastPlateTime = Date.now();
          console.log(`[cooldown] ${COOLDOWN_MS / 1000}s`);
        } else {
          console.log('[plate] none found');
        }
      }
    } catch (err) {
      consecutiveErrors++;
      console.error(`[error] #${consecutiveErrors}: ${err.message}`);
      if (consecutiveErrors >= MAX_RETRIES) {
        console.error('[error] too many failures, waiting 30s');
        await sleep(30_000);
        consecutiveErrors = 0;
      }
    }

    const elapsed = Date.now() - t0;
    if (elapsed < INTERVAL_MS) await sleep(INTERVAL_MS - elapsed);
  }
}

main();
