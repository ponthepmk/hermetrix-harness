//go:build windows

package web

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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
				if !send(map[string]any{"type": "transcribe_error", "message": "ถอดคำพูดจากไมค์ Windows ไม่สำเร็จ"}) {
					return
				}
			} else if result.text != "" {
				if !send(map[string]any{"type": "final", "text": result.text, "language": result.language, "epoch": result.epoch}) {
					return
				}
			} else if !send(map[string]any{"type": "no_speech"}) {
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

func pcmLevel(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	var sum float64
	for i := 0; i+1 < len(pcm); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcm[i:]))
		value := float64(sample) / 32768
		sum += value * value
	}
	return math.Sqrt(sum / float64(len(pcm)/2))
}

func transcribeNativePCM(ctx context.Context, pcm []byte) (string, string, error) {
	select {
	case voiceASRSlot <- struct{}{}:
		defer func() { <-voiceASRSlot }()
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
	whisper, err := findLocalWhisper()
	if err != nil {
		return "", "", err
	}
	if len(pcm) < 3200 || len(pcm) > 640000 {
		return "", "", fmt.Errorf("voice duration is out of range")
	}
	tempDir, err := os.MkdirTemp("", "hermetrix-native-voice-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tempDir)
	wav := make([]byte, 44+len(pcm))
	copy(wav[0:], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 16000)
	binary.LittleEndian.PutUint32(wav[28:], 32000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(pcm)))
	copy(wav[44:], pcm)
	wavPath := filepath.Join(tempDir, "speech.wav")
	if err := os.WriteFile(wavPath, wav, 0600); err != nil {
		return "", "", err
	}
	if endpoint := strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_SERVER")); endpoint != "" {
		value, language, err := transcribeResidentWhisper(ctx, endpoint, wav)
		if err == nil || ctx.Err() != nil {
			return value, language, err
		}
		// A single CPU attempt keeps speech usable if the resident GPU worker
		// fails. The CLI below explicitly disables GPU and shares the same ID.
	}
	prefix := filepath.Join(tempDir, "transcript")
	if _, err := whisper.command(ctx, "-m", whisper.model, "-f", wavPath, "-l", "auto", "--prompt", bilingualVoicePrompt, "-t", "4", "-ng", "-oj", "-of", prefix, "-np").CombinedOutput(); err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(prefix + ".json")
	if err != nil || len(data) > 1<<20 {
		return "", "", fmt.Errorf("invalid local transcript")
	}
	var result struct {
		Result struct {
			Language string `json:"language"`
		} `json:"result"`
		Transcription []struct {
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(data, &result); err != nil || len(result.Transcription) > 200 {
		return "", "", fmt.Errorf("invalid local transcript")
	}
	parts := make([]string, 0, len(result.Transcription))
	for _, segment := range result.Transcription {
		if value := strings.TrimSpace(segment.Text); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " "), result.Result.Language, nil
}

func transcribeResidentWhisper(ctx context.Context, endpoint string, wav []byte) (string, string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" ||
		(parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.Port() == "" {
		return "", "", fmt.Errorf("Whisper server must be a loopback HTTP address")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "speech.wav")
	if err != nil {
		return "", "", err
	}
	if _, err = part.Write(wav); err != nil {
		return "", "", err
	}
	_ = writer.WriteField("response_format", "json")
	_ = writer.WriteField("language", "auto")
	_ = writer.WriteField("translate", "false")
	_ = writer.WriteField("prompt", bilingualVoicePrompt)
	if err = writer.Close(); err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/inference", &body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Whisper server returned %d", response.StatusCode)
	}
	var result struct {
		Text     string `json:"text"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return "", "", err
	}
	return strings.TrimSpace(result.Text), result.Language, nil
}
