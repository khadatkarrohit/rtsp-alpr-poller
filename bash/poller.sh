#!/usr/bin/env bash
# ─────────────────────────────────────────
# ALPR Poller — Bash
# Camera: Prama PT-NC140D3-WNM(D2)
# ─────────────────────────────────────────
# Requires: ffmpeg, curl (both pre-installed on Raspberry Pi OS)
# Run:      chmod +x poller.sh && ./poller.sh

set -euo pipefail

# ── Config (edit these)
RTSP_URL="rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101"
API_KEY="your key, if any"
API_BASE="your api url"
COUNTRY="IND"
INTERVAL_S=2
COOLDOWN_S=8
MAX_RETRIES=3
MOTION_THRESHOLD=30

CURR_FRAME="/tmp/alpr_curr.jpg"
PREV_FRAME="/tmp/alpr_prev.jpg"

last_plate_time=0
consecutive_errors=0

echo "[alpr] bash poller started"
echo "[camera] $RTSP_URL"

# ── Capture one JPEG frame via ffmpeg
capture_frame() {
  [ -f "$CURR_FRAME" ] && mv "$CURR_FRAME" "$PREV_FRAME"
  ffmpeg -loglevel error \
    -rtsp_transport tcp \
    -i "$RTSP_URL" \
    -frames:v 1 -q:v 3 \
    -vf scale=640:480 \
    "$CURR_FRAME" -y 2>/dev/null
}

# ── Motion detection: md5 hash comparison
detect_motion() {
  [ ! -f "$PREV_FRAME" ] && return 1   # no previous → no motion

  h1=$(md5sum "$CURR_FRAME" | awk '{print $1}')
  h2=$(md5sum "$PREV_FRAME" | awk '{print $1}')
  [ "$h1" = "$h2" ] && return 1        # identical → no motion

  # byte-level diff via od — sample every 100 bytes for speed
  diff_score=$(python3 -c "
import sys
a = open('$CURR_FRAME','rb').read()
b = open('$PREV_FRAME','rb').read()
n = min(len(a), len(b))
s = [abs(a[i]-b[i]) for i in range(0, n, 10)]
print(round(sum(s)/len(s)/255*100, 2))
" 2>/dev/null || echo "0")

  echo "[motion] ${diff_score}%"
  # compare with threshold using awk
  awk -v score="$diff_score" -v threshold="$MOTION_THRESHOLD" \
    'BEGIN { exit (score > threshold ? 0 : 1) }'
}

# ── POST frame to ALPR API
scan_plate() {
  curl -s -X POST "${API_BASE}/plate/recognize" \
    -H "x-api-key: ${API_KEY}" \
    -F "image=@${CURR_FRAME}" \
    -F "country=${COUNTRY}"
}

# ── Notify backend after confirmed plate
notify_backend() {
  local plate="$1"
  local confidence="$2"
  local timestamp
  timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

  curl -s -X POST "${API_BASE/\/api\/v1/\/api\/entry}" \
    -H "Content-Type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d "{\"plate\":\"${plate}\",\"confidence\":${confidence},\"country\":\"${COUNTRY}\",\"timestamp\":\"${timestamp}\",\"source\":\"bash-poller-v1\"}" \
    > /dev/null
}

# ── Main loop
while true; do
  t0=$(date +%s%3N)
  now=$(date +%s)

  # Cooldown guard
  if (( now - last_plate_time < COOLDOWN_S )); then
    echo "[cooldown] skipping"
    sleep "$INTERVAL_S"
    continue
  fi

  if capture_frame 2>/dev/null; then
    consecutive_errors=0

    if detect_motion; then
      echo "[motion] detected — scanning plate"
      result=$(scan_plate)
      echo "[alpr] $result"

      plate=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('plate',''))" 2>/dev/null || echo "")
      confidence=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('confidence',0))" 2>/dev/null || echo "0")

      if [ -n "$plate" ]; then
        echo "[plate] $plate (${confidence}%)"
        notify_backend "$plate" "$confidence"
        last_plate_time=$(date +%s)
        echo "[cooldown] ${COOLDOWN_S}s"
      else
        echo "[plate] none found"
      fi
    else
      echo "[motion] none"
    fi
  else
    (( consecutive_errors++ )) || true
    echo "[error] #${consecutive_errors}: ffmpeg failed"
    if (( consecutive_errors >= MAX_RETRIES )); then
      echo "[error] too many failures, waiting 30s"
      sleep 30
      consecutive_errors=0
    fi
  fi

  elapsed=$(( $(date +%s%3N) - t0 ))
  sleep_ms=$(( INTERVAL_S * 1000 - elapsed ))
  [ "$sleep_ms" -gt 0 ] && sleep "$(echo "scale=3; $sleep_ms/1000" | bc)"
done
