//go:build windows

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os/exec"
	"regexp"
	"time"
)

// Only one local browser session may own the Windows microphone at a time.
var nativeMicSlot = make(chan struct{}, 1)
var dshowAudioName = regexp.MustCompile(`"([^"\r\n]{1,200})" \(audio\)`)

func listWindowsMicrophones(ctx context.Context) ([]string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ไม่พบ FFmpeg ในเครื่อง")
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// FFmpeg exits nonzero for the dummy input after listing devices.
	output, _ := exec.CommandContext(probe, ffmpeg, "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy").CombinedOutput()
	unique := make(map[string]bool)
	devices := make([]string, 0, 4)
	for _, match := range dshowAudioName.FindAllSubmatch(output, 32) {
		name := string(match[1])
		if !unique[name] {
			unique[name] = true
			devices = append(devices, name)
		}
	}
	return devices, nil
}

func (s *Server) voiceDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := listWindowsMicrophones(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices, "mode": "local-windows"})
}

type nativeASRResult struct {
	epoch    int
	text     string
	language string
	err      error
}

type micChunk struct {
	pcm []byte
	err error
}

func (s *Server) streamNativeVoice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Device string `json:"device"`
	}
	if !decodeJSONLimit(w, r, &input, 1024) {
		return
	}
	devices, err := listWindowsMicrophones(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	found := false
	for _, name := range devices {
		if input.Device == name {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "เลือกไมโครโฟน Windows ที่พบในรายการ"})
		return
	}
	select {
	case nativeMicSlot <- struct{}{}:
		defer func() { <-nativeMicSlot }()
	default:
		writeJSON(w, http.StatusConflict, map[string]any{"error": "มีการใช้ไมโครโฟน Windows อีกหน้าหนึ่งอยู่"})
		return
	}
	ffmpeg, _ := exec.LookPath("ffmpeg")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-thread_queue_size", "64",
		"-f", "dshow", "-i", "audio="+input.Device, "-ac", "1", "-ar", "16000", "-f", "s16le", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "เปิดไมโครโฟน Windows ไม่ได้"})
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr // Device paths and names are never sent to the browser.
	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "เริ่มไมโครโฟน Windows ไม่ได้"})
		return
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	send := func(event map[string]any) bool {
		if err := encoder.Encode(event); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send(map[string]any{"type": "capture_started", "device": input.Device}) {
		return
	}

	chunks := make(chan micChunk, 16)
	go func() {
		defer close(chunks)
		for {
			block := make([]byte, 3200) // 100 ms of 16 kHz mono PCM16.
			n, readErr := io.ReadFull(stdout, block)
			if n > 0 {
				select {
				case chunks <- micChunk{pcm: block[:n]}:
				case <-ctx.Done():
					return
				}
			}
			if readErr != nil {
				select {
				case chunks <- micChunk{err: readErr}:
				case <-ctx.Done():
				}
				return
			}
		}
	}()

	results := make(chan nativeASRResult, 2)
	var asrCancel context.CancelFunc
	var recent, captured []byte
	var lastLoud time.Time
	var recording bool
	var loudFrames, epoch, levelTicks int
	for {
		select {
		case <-ctx.Done():
			if asrCancel != nil {
				asrCancel()
			}
			return
		case result := <-results:
			if result.epoch != epoch {
				continue
			}
			if result.err != nil {
				if !send(map[string]any{"type": "transcribe_error", "epoch": epoch, "message": "ถอดคำพูดจากไมค์ Windows ไม่สำเร็จ"}) {
					return
				}
			} else if result.text != "" {
				if !send(map[string]any{"type": "final", "text": result.text, "language": result.language, "epoch": result.epoch}) {
					return
				}
			} else if !send(map[string]any{"type": "no_speech", "epoch": epoch}) {
				return
			}
		case chunk, open := <-chunks:
			if !open || chunk.err != nil {
				if ctx.Err() == nil {
					_ = send(map[string]any{"type": "capture_error", "message": "ไมโครโฟนหยุดส่งเสียงหรือถูกถอดออก"})
				}
				return
			}
			now := time.Now()
			level := pcmLevel(chunk.pcm)
			levelTicks++
			if levelTicks >= 2 {
				levelTicks = 0
				if !send(map[string]any{"type": "level", "rms": math.Round(level*1000) / 1000}) {
					return
				}
			}
			loud := level > .012
			if loud {
				loudFrames++
				lastLoud = now
				if !recording && loudFrames >= 2 {
					if asrCancel != nil {
						asrCancel()
						asrCancel = nil
					}
					epoch++
					captured = append(make([]byte, 0, 640000), recent...)
					recording = true
					if !send(map[string]any{"type": "speech_start", "epoch": epoch}) {
						return
					}
				}
			} else {
				loudFrames = 0
			}
			if recording {
				captured = append(captured, chunk.pcm...)
				if now.Sub(lastLoud) > 950*time.Millisecond || len(captured) >= 18*32000 {
					pcm := append([]byte(nil), captured...)
					captured = nil
					recording = false
					if !send(map[string]any{"type": "transcribing", "epoch": epoch}) {
						return
					}
					processCtx, stop := context.WithTimeout(ctx, 40*time.Second)
					asrCancel = stop
					current := epoch
					go func() {
						text, language, err := transcribeNativePCM(processCtx, pcm)
						select {
						case results <- nativeASRResult{epoch: current, text: text, language: language, err: err}:
						case <-ctx.Done():
						}
					}()
				}
			} else {
				recent = append(recent, chunk.pcm...)
				if len(recent) > 6400 {
					recent = append([]byte(nil), recent[len(recent)-6400:]...)
				}
			}
		}
	}
}
