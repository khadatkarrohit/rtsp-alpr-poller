// ─────────────────────────────────────────
// ALPR Poller — Go  ★ OPTIMIZED
// Camera: Prama PT-NC140D3-WNM(D2)
// Tested on: Raspberry Pi 4 Model B — 4 GB RAM
//            Raspberry Pi OS Debian 13 (Trixie)
//            Kernel: Linux 6.18.33-rpt-rpi-v8
// ─────────────────────────────────────────
// Single file, zero external dependencies (stdlib only).
//
// Run:           go run main.go
// Build for Pi:  go build -o alpr-poller .
// See SETUP.md for full installation guide.

package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─── Config ───────────────────────────────────────────────
// Edit these values before running.

const (
	rtspURL        = "rtsp://{username}:{password}@{camera_ip}:554/Streaming/Channels/101"
	cameraIP       = "{camera_ip}"
	cameraUser     = "{camera_username}"
	cameraPass     = "{camera_password}"
	apiKey         = "{your_api_key}"
	apiBase        = "https://alpr.parkese.com/api/v1/plate/recognize"
	country        = "IND"
	cameraProtocol = "polling" // polling | isapi | auto
	logFile        = "/var/log/alpr.log"
)

const (
	cooldownMs       = 4000  // ms to wait after successful detection
	pollIntervalMs   = 2000  // ms between poll cycles
	sceneRatio       = 8     // capture 1 frame every N video frames (~2fps at 15fps)
	pixelThreshold   = 45.0  // % of frame that must change to confirm vehicle
	pixelSensitivity = 10000 // per-pixel RGB diff to count as changed
)

var logger *log.Logger

// ─── Logger ───────────────────────────────────────────────

func initLogger() {
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
		logger.Printf("[warn] log file unavailable (%v) — stdout only", err)
		return
	}
	multi := io.MultiWriter(f, os.Stdout)
	logger = log.New(multi, "", log.LstdFlags)
	logger.Println("[init] logger initialized →", logFile)
}

// ─── Frame Capture (cvlc) ─────────────────────────────────

func cleanFrames() {
	files, _ := filepath.Glob("/tmp/alpr_curr*.jpeg")
	for _, f := range files {
		os.Remove(f)
	}
}

func captureFrames() ([]string, error) {
	cleanFrames()

	cmd := exec.Command("cvlc",
		"--no-audio",
		"--video-filter=scene",
		"--scene-format=jpeg",
		fmt.Sprintf("--scene-ratio=%d", sceneRatio),
		"--scene-prefix=alpr_curr",
		"--scene-path=/tmp",
		"--run-time=2", // 2 seconds = reliable 2 frames at sceneRatio=8
		rtspURL,
		"vlc://quit",
	)
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	files, _ := filepath.Glob("/tmp/alpr_curr*.jpeg")
	return files, nil
}

// ─── Motion Scoring ───────────────────────────────────────
// Pixel-level comparison between two frames.
// Returns % of pixels that changed beyond pixelSensitivity.
// Skips MD5 — JPEG re-encoding always produces slightly different
// bytes even for identical scenes, making MD5 unreliable.

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return jpeg.Decode(f)
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func motionScore(path1, path2 string) (float64, error) {
	img1, err1 := loadImage(path1)
	img2, err2 := loadImage(path2)
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("image load failed: %v | %v", err1, err2)
	}

	bounds := img1.Bounds()
	total := bounds.Dx() * bounds.Dy()
	changed := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r1, g1, b1, _ := img1.At(x, y).RGBA()
			r2, g2, b2, _ := img2.At(x, y).RGBA()
			diff := absInt(int(r1)-int(r2)) +
				absInt(int(g1)-int(g2)) +
				absInt(int(b1)-int(b2))
			if diff > pixelSensitivity {
				changed++
			}
		}
	}

	return float64(changed) / float64(total) * 100, nil
}

// ─── ALPR API ─────────────────────────────────────────────

func callALPR(framePath string) (string, error) {
	file, err := os.Open(framePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("image", filepath.Base(framePath))
	io.Copy(part, file)
	writer.WriteField("country", country)
	writer.Close()

	req, _ := http.NewRequest("POST", apiBase, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return string(respBody), nil
}

// ─── Processing Pipeline ──────────────────────────────────

func processEvent(cycleCount int, frames []string) {
	logger.Printf("[cycle #%d] processing %d frames...", cycleCount, len(frames))

	score, err := motionScore(frames[0], frames[1])
	if err != nil {
		logger.Printf("[error] motion score failed: %v", err)
		cleanFrames()
		return
	}

	logger.Printf("[motion] score=%.2f%% threshold=%.2f%%", score, pixelThreshold)

	if score < pixelThreshold {
		logger.Printf("[filter] %.2f%% below threshold — hand/shadow/bird ignored", score)
		cleanFrames()
		return
	}

	logger.Printf("[alpr] 🚗 vehicle confirmed (%.2f%%) — calling ALPR API", score)

	respBody, err := callALPR(frames[0])
	if err != nil {
		logger.Printf("[error] ALPR API failed: %v", err)
		cleanFrames()
		return
	}

	logger.Printf("[alpr] ✅ SUCCESS — %s", respBody)
	cleanFrames()
	logger.Printf("[cooldown] waiting %dms...", cooldownMs)
	time.Sleep(time.Duration(cooldownMs) * time.Millisecond)
}

// ─── Mode: RTSP Frame Polling ─────────────────────────────
// Works with ANY IP camera that has an RTSP stream.
// Captures frames every pollIntervalMs, scoring is done in processEvent.

func pollFrames() {
	logger.Println("[polling] RTSP frame polling started")
	cycleCount := 0

	for {
		cycleCount++

		frames, err := captureFrames()
		if err != nil || len(frames) < 2 {
			logger.Printf("[polling #%d] capture failed (got %d frames): %v",
				cycleCount, len(frames), err)
			cleanFrames()
			time.Sleep(time.Duration(pollIntervalMs) * time.Millisecond)
			continue
		}

		processEvent(cycleCount, frames)
		time.Sleep(time.Duration(pollIntervalMs) * time.Millisecond)
	}
}

// ─── Mode: ISAPI Alert Stream ─────────────────────────────
// For cameras with ISAPI support.
// Camera pushes XML events when it detects vehicle motion (VMD).
// Uses InsecureSkipVerify — camera self-signed cert, safe on LAN.

func listenISAPI(triggerCh chan<- []string) {
	url := fmt.Sprintf("https://%s/ISAPI/Event/notification/alertStream", cameraIP)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	for {
		logger.Println("[isapi] connecting to camera alert stream...")

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			logger.Printf("[isapi] request error: %v — retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		req.SetBasicAuth(cameraUser, cameraPass)

		client := &http.Client{Timeout: 0, Transport: transport}
		resp, err := client.Do(req)
		if err != nil {
			logger.Printf("[isapi] connection failed: %v — retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		logger.Println("[isapi] connected ✅")

		buf := make([]byte, 4096)
		var accumulated string

		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				accumulated += string(buf[:n])

				if strings.Contains(accumulated, "VMD") &&
					strings.Contains(accumulated, "active") {
					logger.Println("[isapi] 🚗 VMD vehicle event received!")
					frames, captureErr := captureFrames()
					if captureErr == nil && len(frames) >= 2 {
						select {
						case triggerCh <- frames:
						default:
							logger.Println("[isapi] trigger busy — skipping")
							cleanFrames()
						}
					} else {
						logger.Printf("[isapi] capture after event failed: %v", captureErr)
					}
					accumulated = ""
				}

				if strings.Contains(accumulated, "heartbeat") {
					logger.Println("[isapi] ♥ heartbeat")
					accumulated = ""
				}

				if len(accumulated) > 8192 {
					accumulated = accumulated[4096:]
				}
			}
			if err != nil {
				logger.Printf("[isapi] stream error: %v — reconnecting", err)
				break
			}
		}
		resp.Body.Close()
		time.Sleep(2 * time.Second)
	}
}

// ─── Main ─────────────────────────────────────────────────

func main() {
	initLogger()
	logger.Println("[alpr] 🚀 Parkese Edge Node v2.0 started")
	logger.Printf("[config] protocol=%s camera=%s threshold=%.0f%% cooldown=%dms interval=%dms",
		cameraProtocol, cameraIP, pixelThreshold, cooldownMs, pollIntervalMs)

	switch cameraProtocol {

	case "polling":
		pollFrames()

	case "isapi":
		triggerCh := make(chan []string, 1)
		go listenISAPI(triggerCh)
		cycleCount := 0
		for frames := range triggerCh {
			cycleCount++
			processEvent(cycleCount, frames)
		}

	default: // "auto"
		logger.Println("[auto] checking ISAPI availability...")
		testURL := fmt.Sprintf("https://%s/ISAPI/System/status", cameraIP)
		req, _ := http.NewRequest("GET", testURL, nil)
		req.SetBasicAuth(cameraUser, cameraPass)
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Timeout: 5 * time.Second, Transport: transport}
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			logger.Println("[auto] ISAPI detected → isapi mode")
			triggerCh := make(chan []string, 1)
			go listenISAPI(triggerCh)
			cycleCount := 0
			for frames := range triggerCh {
				cycleCount++
				processEvent(cycleCount, frames)
			}
		} else {
			logger.Println("[auto] ISAPI not available → polling mode")
			pollFrames()
		}
	}
}
