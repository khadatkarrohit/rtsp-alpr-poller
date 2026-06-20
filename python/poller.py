#!/usr/bin/env python3
"""
RTSP ALPR Poller — Python
Captures frames via OpenCV and sends them to the ALPR API.

Usage:
    RTSP_URL=rtsp://... API_KEY=... COUNTRY=IND python poller.py
"""

import time
import logging
import requests
import cv2

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(message)s",
    datefmt="%Y-%m-%dT%H:%M:%S",
)
log = logging.getLogger(__name__)

RTSP_URL   = "rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101"
API_KEY    = "your key, if any"
API_BASE   = "your api url"
COUNTRY    = "IND"
INTERVAL_S = 2.0


def open_stream(url: str) -> cv2.VideoCapture:
    cap = cv2.VideoCapture(url, cv2.CAP_FFMPEG)
    cap.set(cv2.CAP_PROP_BUFFERSIZE, 1)
    if not cap.isOpened():
        raise RuntimeError(f"cannot open stream: {url}")
    return cap


def capture_frame(cap: cv2.VideoCapture) -> bytes:
    # drain stale buffered frames
    for _ in range(3):
        cap.grab()
    ok, frame = cap.read()
    if not ok:
        raise RuntimeError("failed to read frame")
    _, buf = cv2.imencode(".jpg", frame, [cv2.IMWRITE_JPEG_QUALITY, 85])
    return buf.tobytes()


def recognize_plate(image_bytes: bytes) -> dict:
    resp = requests.post(
        f"{API_BASE}/plate/recognize",
        headers={"x-api-key": API_KEY},
        files={"image": ("frame.jpg", image_bytes, "image/jpeg")},
        data={"country": COUNTRY},
        timeout=10,
    )
    resp.raise_for_status()
    return resp.json()


def main():
    log.info("starting python poller — rtsp: %s, interval: %.1fs", RTSP_URL, INTERVAL_S)
    cap = open_stream(RTSP_URL)

    try:
        while True:
            t0 = time.time()
            try:
                frame = capture_frame(cap)
                result = recognize_plate(frame)
                elapsed = (time.time() - t0) * 1000
                log.info("%.0fms %s", elapsed, result)
            except Exception as exc:
                log.error("poll error: %s", exc)
                # attempt reconnect
                cap.release()
                time.sleep(2)
                cap = open_stream(RTSP_URL)

            time.sleep(max(0, INTERVAL_S - (time.time() - t0)))
    finally:
        cap.release()


if __name__ == "__main__":
    main()
