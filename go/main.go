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
	"crypto/md5"
	"encoding/xml"
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
	"time"
)

// ── Config (edit these before running)
const (
	rtspURL          = "rtsp://{username}:{password}@{camera_ip}:{port}/Streaming/Channels/101"
	cameraIP         = "{camera_ip}"
	cameraUser       = "{camera_username}"
	cameraPass       = "{camera_password}"
	apiKey           = "your key, if any"
	apiBase          = "your api url"
	country          = "IND"
	cooldownMs       = 4000
	sceneRatio       = 8       // capture 1 frame every N video frames
	pixelThreshold   = 25.0   // % of frame pixels that must change to confirm motion
	pixelSensitivity = 10000  // per-pixel diff sensitivity
	logFile          = "/var/log/alpr.log"
)

var logger *log.Logger

// ─── Logger ───────────────────────────────────────────────

func initLogger() {
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger = log.New(os.Stdout, "", log.LstdFlags)
		logger.Printf("[warn] could not open log file: %v", err)
		return
	}
	multi := io.MultiWriter(f, os.Stdout)
	logger = log.New(multi, "", log.LstdFlags)
	logger.Println("[init] logger initialized →", logFile)
}

// ─── Camera Event Listener (Prama/Hikvision ISAPI) ────────
// Connects to the camera's built-in alert stream.
// The camera pushes an XML event when it detects a vehicle (VMD).
// This replaces blind polling — API is only called on a real event.

type EventNotification struct {
	EventType        string `xml:"eventType"`
	EventState       string `xml:"eventState"`
	EventDescription string `xml:"eventDescription"`
}

func listenCameraEvents(triggerCh chan<- bool) {
	url := fmt.Sprintf("http://%s/ISAPI/Event/notification/alertStream", cameraIP)

	for {
		logger.Println("[event] connecting to camera alert stream...")

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			logger.Printf("[event] request error: %v — retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		req.SetBasicAuth(cameraUser, cameraPass)

		client := &http.Client{Timeout: 0} // no timeout — long-lived stream
		resp, err := client.Do(req)
		if err != nil {
			logger.Printf("[event] connection failed: %v — retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}

		logger.Println("[event] connected to camera alert stream")

		decoder := xml.NewDecoder(resp.Body)
		for {
			var event EventNotification
			if err := decoder.Decode(&event); err != nil {
				logger.Printf("[event] stream error: %v — reconnecting", err)
				break
			}

			logger.Printf("[event] received: type=%s state=%s desc=%s",
				event.EventType, event.EventState, event.EventDescription)

			// Trigger only on VMD (Video Motion Detection) active events
			if event.EventType == "VMD" && event.EventState == "active" {
				logger.Println("[event] vehicle motion event from camera")
				select {
				case triggerCh <- true:
				default:
					logger.Println("[event] trigger already pending, skipping")
				}
			}
		}
		resp.Body.Close()
		time.Sleep(2 * time.Second)
	}
}

// ─── Frame Capture (VLC) ──────────────────────────────────
// Uses cvlc scene filter to capture multiple JPEG frames
// from the RTSP stream in one 1-second burst.

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
		"--run-time=1",
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

// ─── Layer 1: MD5 Quick Check ─────────────────────────────
// Fast identical-frame filter before expensive pixel comparison.

func md5File(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := md5.New()
	io.Copy(h, f)
	return h.Sum(nil), nil
}

func framesAreDifferent(f1, f2 string) bool {
	h1, err1 := md5File(f1)
	h2, err2 := md5File(f2)
	if err1 != nil || err2 != nil {
		return false
	}
	return !bytes.Equal(h1, h2)
}

// ─── Layer 2: Pixel Area Threshold ────────────────────────
// Counts how many pixels changed significantly between two frames.
// Returns % of total pixels changed. Filters out shadows, birds,
// headlights, and small false positives.

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

	score := float64(changed) / float64(total) * 100
	return score, nil
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

func processEvent(cycleCount int) {
	logger.Printf("[cycle #%d] vehicle event received — capturing frames...", cycleCount)

	frames, err := captureFrames()
	if err != nil || len(frames) < 2 {
		logger.Printf("[error] capture failed (got %d frames): %v", len(frames), err)
		cleanFrames()
		return
	}

	logger.Printf("[capture] got %d frames", len(frames))

	// Layer 1: Quick MD5 check
	if !framesAreDifferent(frames[0], frames[1]) {
		logger.Println("[filter] md5 identical — false trigger, skipping")
		cleanFrames()
		return
	}

	// Layer 2: Pixel area threshold
	score, err := motionScore(frames[0], frames[1])
	if err != nil {
		logger.Printf("[error] motion score failed: %v", err)
		cleanFrames()
		return
	}

	logger.Printf("[motion] change score: %.2f%% (threshold: %.2f%%)", score, pixelThreshold)

	if score < pixelThreshold {
		logger.Printf("[filter] score %.2f%% below threshold — shadow/bird/headlight ignored", score)
		cleanFrames()
		return
	}

	// Both layers passed — call ALPR API
	logger.Printf("[alpr] score %.2f%% confirmed — sending frame to ALPR API", score)

	respBody, err := callALPR(frames[0])
	if err != nil {
		logger.Printf("[error] ALPR API failed: %v", err)
		cleanFrames()
		return
	}

	logger.Printf("[alpr] response: %s", respBody)
	cleanFrames()
	logger.Printf("[cooldown] waiting %dms", cooldownMs)
	time.Sleep(time.Duration(cooldownMs) * time.Millisecond)
}

// ─── Main ─────────────────────────────────────────────────

func main() {
	initLogger()
	logger.Println("[alpr] poller started")
	logger.Printf("[config] camera=%s threshold=%.0f%% cooldown=%dms",
		cameraIP, pixelThreshold, cooldownMs)

	// Buffered channel — holds one pending trigger
	triggerCh := make(chan bool, 1)

	// Camera event listener runs in background
	// When camera detects a vehicle (VMD event), it pushes to triggerCh
	go listenCameraEvents(triggerCh)

	cycleCount := 0
	for range triggerCh {
		cycleCount++
		processEvent(cycleCount)
	}
}
