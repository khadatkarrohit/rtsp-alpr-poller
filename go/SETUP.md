# Go Poller — Setup Guide

**Tested on:**
- Hardware: Raspberry Pi 4 Model B — 4 GB RAM
- OS: Raspberry Pi OS Debian 13 (Trixie)
- Kernel: Linux 6.18.33-rpt-rpi-v8 (64-bit ARM)

---

## Prerequisites

### 1. Update system

```bash
sudo apt update && sudo apt upgrade -y
```

### 2. Install ffmpeg

```bash
sudo apt install -y ffmpeg
ffmpeg -version
```

### 3. Install Go

Debian 13 Trixie ships with Go but it may not be the latest.
Install directly from the official source for best compatibility:

```bash
# Download Go for ARM64 (matches rpi-v8 64-bit kernel)
wget https://go.dev/dl/go1.22.3.linux-arm64.tar.gz

# Remove any existing Go installation
sudo rm -rf /usr/local/go

# Extract
sudo tar -C /usr/local -xzf go1.22.3.linux-arm64.tar.gz

# Add to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify
go version
# Expected: go version go1.22.3 linux/arm64
```

---

## Setup

### 1. Clone the repo

```bash
git clone https://github.com/khadatkarrohit/rtsp-alpr-poller.git
cd rtsp-alpr-poller/go
```

### 2. Edit config

Open `main.go` and fill in your values at the top of the file:

```bash
nano main.go
```

Edit these constants:

```go
rtspURL  = "rtsp://..."        // your camera RTSP URL
apiKey   = "..."               // your ALPR API key
apiBase  = "..."               // your ALPR API base URL
country  = "IND"               // your country code
```

Save: `Ctrl+X` → `Y` → `Enter`

### 3. Run

```bash
go run main.go
```

---

## Run as a service (auto-start on boot)

### 1. Build a binary first

```bash
cd ~/rtsp-alpr-poller/go
go build -o alpr-poller .
```

### 2. Create systemd service

```bash
sudo nano /etc/systemd/system/alpr-poller.service
```

Paste the following:

```ini
[Unit]
Description=ALPR Go Poller
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/home/pi/rtsp-alpr-poller/go/alpr-poller
WorkingDirectory=/home/pi/rtsp-alpr-poller/go
Restart=always
RestartSec=5
User=pi
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

### 3. Enable and start

```bash
sudo systemctl daemon-reload
sudo systemctl enable alpr-poller
sudo systemctl start alpr-poller
```

### 4. Check status and logs

```bash
# Status
sudo systemctl status alpr-poller

# Live logs
sudo journalctl -u alpr-poller -f
```

### 5. Stop / restart

```bash
sudo systemctl stop alpr-poller
sudo systemctl restart alpr-poller
```

---

## Troubleshooting

**ffmpeg: command not found**
```bash
sudo apt install -y ffmpeg
```

**go: command not found**
```bash
source ~/.bashrc
# or open a new terminal
```

**Cannot connect to RTSP stream**
- Check camera is on the same network as the Pi
- Test the RTSP URL with VLC first: `vlc rtsp://...`
- Make sure port 554 is not blocked

**Permission denied on service**
- Check the `User=pi` in the service file matches your actual username
- Run `whoami` to confirm your username
