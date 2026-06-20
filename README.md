# rtsp-alpr-poller

RTSP frame pollers for Raspberry Pi. Each poller captures a frame from a camera stream, detects motion, and sends the frame to an ALPR API to read the licence plate.

> **Tested and working on Raspberry Pi 4 (4 GB RAM).**  
> Successfully detects vehicles from a live RTSP stream, captures the frame, and sends it to the ALPR API for licence plate recognition.

**Hardware tested:** Raspberry Pi 4 Model B — 4 GB RAM  
**Camera tested:** Prama PT-NC140D3-WNM(D2)  
**RTSP format:** `rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101`

---

## How every poller works

```
Camera RTSP stream
  → ffmpeg grabs one frame every 2 s
  → MD5 + pixel-diff motion detection  (skip if < 30 % change)
  → POST frame → ALPR API  /plate/recognize
  → If plate found → POST → backend  /api/entry
  → 8 s cooldown to avoid duplicate scans
  → Auto-retry with 30 s backoff after 3 consecutive failures
```

---

## Language comparison

| Language   | Single file | Pi Zero 2 W | Pi 4  | Speed     | Recommended  |
|------------|-------------|-------------|-------|-----------|--------------|
| **Go**     | ✅ yes       | ✅ best      | ✅    | Very fast | ★ Optimized  |
| **Rust**   | ⚠️ + Cargo   | ✅ excellent | ✅    | Fastest   | ★ Optimized  |
| **Node.js**| ✅ yes       | ⚠️ marginal  | ✅    | Fast      | ★ Optimized  |
| Bun        | ✅ yes       | ⚠️ marginal  | ✅    | Fast      | Good         |
| Python     | ✅ yes       | ⚠️ marginal  | ✅    | Medium    | Good         |
| Dart       | ✅ yes       | ⚠️ marginal  | ✅    | Fast      | Good         |
| Elixir     | ✅ yes       | ⚠️ heavy     | ✅    | Fast      | Good         |
| Bash       | ✅ yes       | ✅ yes       | ✅    | Slow      | Simplest     |
| C++        | ⚠️ + cmake   | ✅ best      | ✅    | Fastest   | Complex      |
| Java       | ❌ + Maven   | ❌ avoid     | ⚠️    | Medium    | Not ideal    |

> **★ Optimized** = recommended for production use on Raspberry Pi.  
> **Go** is the top pick — single binary, zero external deps, lowest CPU on Pi, trivial cross-compile.

---

## Config — edit directly in each file

Every poller has these four variables at the top of the file. Edit them before running.

```
RTSP_URL  =  rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101
API_KEY   =  your key, if any
API_BASE  =  your api url
COUNTRY   =  IND
```

No environment variables, no config files — just open the poller file and change the values.

---

## Step-by-step setup

---

### 1. Go ★ Optimized

**Why:** Single static binary, zero external dependencies, lowest CPU on Pi, easy cross-compile.

#### Step 1 — Install Go on Raspberry Pi

```bash
# Pi 4 / Pi 5 (64-bit OS)
wget https://go.dev/dl/go1.22.3.linux-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.22.3.linux-arm64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version
```

#### Step 2 — Install ffmpeg

```bash
sudo apt update && sudo apt install -y ffmpeg
```

#### Step 3 — Edit config

Open `go/main.go` and set `rtspURL`, `apiKey`, `apiBase` at the top.

#### Step 4 — Run

```bash
cd go
go run main.go
```

#### Step 5 — (Optional) Cross-compile from Mac/Linux and deploy to Pi

```bash
GOOS=linux GOARCH=arm64 go build -o poller .
scp poller pi@<pi-ip>:~/
ssh pi@<pi-ip> ./poller
```

---

### 2. Rust ★ Optimized

**Why:** Fastest execution, zero GC pauses, memory safe. Best for Pi Zero 2 W.

#### Step 1 — Install Rust on Raspberry Pi

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source ~/.cargo/env
rustc --version
```

#### Step 2 — Install ffmpeg

```bash
sudo apt update && sudo apt install -y ffmpeg
```

#### Step 3 — Edit config

Open `rust/src/main.rs` and set `RTSP_URL`, `API_KEY`, `API_BASE` constants at the top.

#### Step 4 — Run

```bash
cd rust
cargo run --release
```

#### Step 5 — (Optional) Cross-compile from Mac/Linux

```bash
rustup target add aarch64-unknown-linux-gnu
cargo build --target aarch64-unknown-linux-gnu --release
scp target/aarch64-unknown-linux-gnu/release/poller pi@<pi-ip>:~/
ssh pi@<pi-ip> ./poller
```

---

### 3. Node.js ★ Optimized

**Why:** Zero npm dependencies on Node 18+ (`fetch` and `FormData` are built-in), easy to read and modify.

#### Step 1 — Install Node.js 18+ on Raspberry Pi

```bash
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt install -y nodejs
node --version   # must show v18 or higher
```

#### Step 2 — Install ffmpeg

```bash
sudo apt install -y ffmpeg
```

#### Step 3 — Edit config

Open `nodejs/poller.js` and set `RTSP_URL`, `API_KEY`, `API_BASE` at the top.

#### Step 4 — Run

```bash
cd nodejs

# Option A — rename to .mjs (recommended, cleanest)
mv poller.js poller.mjs
node poller.mjs

# Option B — run directly as ES module
node --input-type=module < poller.js
```

---

### 4. Bun

**Why:** Drop-in Node.js replacement, 3× faster startup, TypeScript native, no transpile step.

#### Step 1 — Install Bun on Raspberry Pi

```bash
curl -fsSL https://bun.sh/install | bash
source ~/.bashrc
bun --version
```

#### Step 2 — Install ffmpeg

```bash
sudo apt install -y ffmpeg
```

#### Step 3 — Edit config

Open `bun/poller.ts` and set `RTSP_URL`, `API_KEY`, `API_BASE` at the top.

#### Step 4 — Run

```bash
cd bun
bun run poller.ts
```

---

### 5. Python

**Why:** Easiest to modify and prototype. Large OpenCV community on Pi.

#### Step 1 — Check Python version (pre-installed on Raspberry Pi OS)

```bash
python3 --version   # must be 3.9 or higher
```

#### Step 2 — Install dependencies

```bash
pip3 install requests opencv-python-headless
```

> `opencv-python-headless` saves ~50 MB RAM vs the full opencv package.

#### Step 3 — Install ffmpeg

```bash
sudo apt install -y ffmpeg
```

#### Step 4 — Edit config

Open `python/poller.py` and set `RTSP_URL`, `API_KEY`, `API_BASE` at the top.

#### Step 5 — Run

```bash
cd python
python3 poller.py
```

---

### 6. Dart

**Why:** Single file, fast AOT-compiled binary, no VM overhead after compilation.

#### Step 1 — Install Dart on Raspberry Pi

```bash
sudo apt update && sudo apt install -y apt-transport-https
wget -qO- https://dl-ssl.google.com/linux/linux_signing_key.pub \
  | sudo gpg --dearmor -o /usr/share/keyrings/dart.gpg
echo 'deb [signed-by=/usr/share/keyrings/dart.gpg] https://storage.googleapis.com/download.dartlang.org/linux/debian stable main' \
  | sudo tee /etc/apt/sources.list.d/dart_stable.list
sudo apt update && sudo apt install -y dart
export PATH="$PATH:/usr/lib/dart/bin"
dart --version
```

#### Step 2 — Install ffmpeg

```bash
sudo apt install -y ffmpeg
```

#### Step 3 — Edit config

Open `dart/poller.dart` and set `rtspUrl`, `apiKey`, `apiBase` at the top.

#### Step 4 — Run

```bash
cd dart

# Interpreted (slower, good for testing)
dart run poller.dart

# Compile to native binary first (faster on Pi)
dart compile exe poller.dart -o poller
./poller
```

---

### 7. Elixir

**Why:** Excellent concurrency model (BEAM VM), great if you later want to run multiple cameras.

#### Step 1 — Install Elixir on Raspberry Pi

```bash
sudo apt update && sudo apt install -y elixir
elixir --version
```

#### Step 2 — Install ffmpeg

```bash
sudo apt install -y ffmpeg
```

#### Step 3 — Edit config

Open `elixir/poller.exs` and set `@rtsp_url`, `@api_key`, `@api_base` at the top of the module.

#### Step 4 — Run

```bash
cd elixir
elixir poller.exs
```

> First run downloads the `req` HTTP library automatically via `Mix.install` — internet required.

---

### 8. Bash

**Why:** Simplest possible poller. ffmpeg and curl are already on Raspberry Pi OS — zero install.

#### Step 1 — Install ffmpeg + curl (usually pre-installed)

```bash
sudo apt install -y ffmpeg curl
```

#### Step 2 — Edit config

Open `bash/poller.sh` and set `RTSP_URL`, `API_KEY`, `API_BASE` at the top.

#### Step 3 — Run

```bash
cd bash
chmod +x poller.sh
./poller.sh
```

---

### 9. C++

**Why:** Absolute lowest overhead. Best when running multiple processes on Pi Zero 2 W.

#### Step 1 — Install dependencies on Raspberry Pi

```bash
sudo apt update && sudo apt install -y \
  build-essential cmake \
  libopencv-dev \
  libcurl4-openssl-dev
```

#### Step 2 — Edit config

Open `cpp/poller.cpp` and set `rtsp_url`, `api_key`, `api_base` in `main()`.

#### Step 3 — Build and run

```bash
cd cpp
mkdir build && cd build
cmake .. && make -j$(nproc)
./poller
```

---

### 10. Java

**Why:** Only recommended if your team already has a Java codebase. Avoid on Pi Zero 2 W.

#### Step 1 — Install Java 17 + Maven on Raspberry Pi

```bash
sudo apt update && sudo apt install -y openjdk-17-jdk maven
java --version
mvn --version
```

#### Step 2 — Edit config

Open `java/src/main/java/com/alpr/RtspPoller.java` and set `rtspUrl`, `apiKey`, `apiBase` in `main()`.

#### Step 3 — Build

```bash
cd java
mvn package -q
```

#### Step 4 — Run

```bash
java -jar target/rtsp-alpr-poller-1.0.0.jar
```

> JavaCV bundles its own ffmpeg — no separate install needed.

---

## Run as a background service on Raspberry Pi (all languages)

Create a systemd service so the poller restarts automatically on reboot or crash:

```bash
sudo nano /etc/systemd/system/alpr-poller.service
```

```ini
[Unit]
Description=ALPR Poller
After=network.target

[Service]
# Change ExecStart to match your chosen language:
# Go:     ExecStart=/home/pi/go/poller
# Node:   ExecStart=/usr/bin/node /home/pi/nodejs/poller.mjs
# Python: ExecStart=/usr/bin/python3 /home/pi/python/poller.py
# Bash:   ExecStart=/bin/bash /home/pi/bash/poller.sh
ExecStart=/home/pi/go/poller
WorkingDirectory=/home/pi
Restart=always
RestartSec=5
User=pi

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable alpr-poller
sudo systemctl start alpr-poller

# View live logs
sudo journalctl -u alpr-poller -f
```
