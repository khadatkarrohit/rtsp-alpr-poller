// ─────────────────────────────────────────
// ALPR Poller — Go  ★ OPTIMIZED
// Camera: Prama PT-NC140D3-WNM(D2)
// ─────────────────────────────────────────
// Single file, zero external dependencies (stdlib only).
//
// Run on Pi:        go run main.go
// Cross-compile:    GOOS=linux GOARCH=arm64 go build -o poller .
//                   scp poller pi@<ip>:~/ && ssh pi@<ip> ./poller

package main

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// ── Config (edit these)
const (
	rtspURL         = "rtsp://{username}:{password}@{your_ip}:{port}/Streaming/channels/101"
	apiKey          = "your key, if any"
	apiBase         = "your api url"
	country         = "IND"
	intervalMs      = 2000
	cooldownMs      = 4000
	motionThreshold = 30.0
	maxRetries      = 3
)

const currFrame = "/tmp/alpr_curr.jpg"
const prevFrame = "/tmp/alpr_prev.jpg"

// ── Capture one JPEG frame via ffmpeg
func captureFrame() (curr []byte, prev []byte, err error) {
	if _, e := os.Stat(currFrame); e == nil {
		os.Rename(currFrame, prevFrame)
	}

	cmd := exec.Command("ffmpeg",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-frames:v", "1",
		"-q:v", "3",
		"-vf", "scale=640:480",
		currFrame, "-y",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	done := make(chan error, 1)
	if err = cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("ffmpeg start: %w", err)
	}
	go func() { done <- cmd.Wait() }()

	select {
	case e := <-done:
		if e != nil {
			return nil, nil, fmt.Errorf("ffmpeg: %w", e)
		}
	case <-time.After(6 * time.Second):
		cmd.Process.Kill()
		return nil, nil, fmt.Errorf("ffmpeg timeout")
	}

	curr, err = os.ReadFile(currFrame)
	if err != nil {
		return nil, nil, err
	}
	if _, e := os.Stat(prevFrame); e == nil {
		prev, _ = os.ReadFile(prevFrame)
	}
	return curr, prev, nil
}

// ── Motion detection: MD5 check + sampled byte diff score
func detectMotion(current, previous []byte) bool {
	if previous == nil {
		return false
	}
	if md5.Sum(current) == md5.Sum(previous) {
		return false
	}
	length := len(current)
	if len(previous) < length {
		length = len(previous)
	}
	var diff, samples float64
	for i := 0; i < length; i += 10 {
		diff += math.Abs(float64(current[i]) - float64(previous[i]))
		samples++
	}
	score := (diff / samples) / 255.0 * 100.0
	log.Printf("[motion] %.2f%%", score)
	return score > motionThreshold
}

// ── POST frame to ALPR API
func scanPlate(client *http.Client) (map[string]any, error) {
	data, err := os.ReadFile(currFrame)
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("image", "frame.jpg")
	fw.Write(data)
	w.WriteField("country", country)
	w.Close()

	req, _ := http.NewRequest(http.MethodPost, apiBase+"/plate/recognize", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("x-api-key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ALPR API %d", resp.StatusCode)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// ── Notify backend after confirmed plate
func notifyBackend(client *http.Client, result map[string]any) {
	payload, _ := json.Marshal(map[string]any{
		"plate":      result["plate"],
		"confidence": result["confidence"],
		"country":    country,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"source":     "go-poller-v3",
	})
	req, _ := http.NewRequest(http.MethodPost, apiBase+"/entry", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// ── Main loop
func main() {
	log.Println("[alpr] go poller started")
	log.Printf("[camera] %s", rtspURL)

	client := &http.Client{Timeout: 10 * time.Second}
	interval := time.Duration(intervalMs) * time.Millisecond
	cooldown := time.Duration(cooldownMs) * time.Millisecond

	var lastPlate time.Time
	errors := 0

	for {
		t0 := time.Now()

		if !lastPlate.IsZero() && time.Since(lastPlate) < cooldown {
			log.Println("[cooldown] skipping")
			time.Sleep(interval)
			continue
		}

		curr, prev, err := captureFrame()
		if err != nil {
			errors++
			log.Printf("[error] #%d: %v", errors, err)
			if errors >= maxRetries {
				log.Println("[error] too many failures, waiting 30s")
				time.Sleep(30 * time.Second)
				errors = 0
			}
			time.Sleep(interval)
			continue
		}
		errors = 0

		if !detectMotion(curr, prev) {
			log.Println("[motion] none")
		} else {
			log.Println("[motion] detected — scanning plate")
			result, err := scanPlate(client)
			if err != nil {
				log.Printf("[error] alpr: %v", err)
			} else {
				log.Printf("[alpr] %v", result)
				if plate, ok := result["plate"].(string); ok && plate != "" {
					log.Printf("[plate] %s (%.0f%%)", plate, result["confidence"])
					notifyBackend(client, result)
					lastPlate = time.Now()
					log.Printf("[cooldown] %ds", cooldownMs/1000)
				} else {
					log.Println("[plate] none found")
				}
			}
		}

		if elapsed := time.Since(t0); elapsed < interval {
			time.Sleep(interval - elapsed)
		}
	}
}
