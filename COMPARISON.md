# Language Comparison — RTSP ALPR Poller on Raspberry Pi

All numbers are **estimates** based on RTSP frame capture + JPEG encode + HTTP POST
on a Pi 4 (4 GB) and Pi Zero 2 W. Real figures depend on camera resolution,
network latency to the API, and system load.

## Benchmark Summary

| Language | Frame Grab (ms) | Total Latency (ms) | RAM (MB) | CPU % (Pi 4) | CPU % (Pi Zero 2 W) | Pi Zero 2 W viable? |
|----------|-----------------|--------------------|----------|--------------|---------------------|---------------------|
| **C++**  | ~8              | ~80–120            | ~25      | 5–8 %        | 20–30 %             | ✅ best choice       |
| **Rust** | ~10             | ~90–130            | ~8       | 6–9 %        | 22–32 %             | ✅ excellent          |
| **Go**   | ~12             | ~100–150           | ~18      | 7–10 %       | 25–35 %             | ✅ very good          |
| **Python** (OpenCV) | ~15 | ~110–160        | ~60      | 10–15 %      | 35–50 %             | ⚠️ marginal          |
| **Node.js** | ~25 (ffmpeg subprocess) | ~150–200 | ~70  | 12–18 %  | 40–55 %             | ⚠️ marginal          |
| **Java** | ~30             | ~200–300           | ~150–250 | 15–22 %      | ❌ too heavy         | ❌ avoid              |

> Frame Grab = time to pull one JPEG out of the RTSP stream.  
> Total Latency = end-to-end from grab to API response received.

---

## Detailed Analysis

### C++ — Fastest, lowest RAM
- **When to use**: Pi Zero 2 W, latency-critical deployments, running alongside other processes.
- **Pros**: NEON SIMD auto-vectorisation on ARM, zero GC pauses, minimal dependencies.
- **Cons**: Longest build cycle; cross-compilation setup required.
- **Deps**: `libopencv-dev`, `libcurl4-openssl-dev`

### Rust — Best developer experience at near-C speed
- **When to use**: Pi 4 or Pi Zero 2 W, teams that want safety guarantees.
- **Pros**: Memory-safe, no GC, async via Tokio, cross-compile with a single flag.
- **Cons**: Compile time is long on-device; cross-compile recommended.
- **Build for Pi**: `cargo build --target aarch64-unknown-linux-gnu --release`

### Go — Best balance of speed and simplicity
- **When to use**: Pi 4, fast iteration, small teams.
- **Pros**: Static binary, trivial cross-compile, goroutines scale well, tiny runtime.
- **Cons**: GC can cause ~1–5 ms pauses (acceptable for ALPR polling intervals).
- **Build for Pi**: `GOOS=linux GOARCH=arm64 go build -o poller ./main.go`

### Python — Easiest to prototype, moderate performance
- **When to use**: Pi 4 with headroom to spare, rapid prototyping, data-science team.
- **Pros**: OpenCV on Pi is well-documented; pip install just works.
- **Cons**: GIL, interpreter overhead, high RAM. Struggles on Pi Zero 2 W at <2 s intervals.
- **Deps**: `pip install -r requirements.txt`

### Node.js — Good ecosystem, subprocess overhead
- **When to use**: Pi 4 when the team already owns Node.js.
- **Pros**: Large npm ecosystem, async I/O is first class.
- **Cons**: Spawns a new `ffmpeg` process each frame — that's 20–40 ms overhead per grab.
  Use `node-rtsp-stream` or a persistent ffmpeg pipe for production.
- **Deps**: `npm install`

### Java — Avoid on Pi Zero 2 W
- **When to use**: Pi 4 only, existing Java codebase.
- **Pros**: JavaCV wraps FFmpeg well; Java 17+ is fast.
- **Cons**: JVM startup ~400 ms, heap floors at ~150 MB. Completely unusable on Pi Zero 2 W.
- **Build**: `mvn package` → `java -jar target/rtsp-alpr-poller-1.0.0.jar`

---

## Recommendation by Hardware

| Hardware        | Recommended Language | Reason |
|-----------------|---------------------|--------|
| Pi Zero 2 W     | **C++** or **Rust** | Tight memory + CPU budget |
| Pi 3 Model B    | **Go** or **Rust**  | Good balance |
| Pi 4 / Pi 5     | **Go** or **Python**| Fast enough, easiest ops |
| Pi Compute Module 4 | **Rust** or **C++** | Embedded, production |

---

## Setup: ffmpeg on Raspberry Pi OS

All pollers (except Python/C++ which use OpenCV directly) require ffmpeg:

```bash
sudo apt update && sudo apt install -y ffmpeg libopencv-dev libcurl4-openssl-dev
```
