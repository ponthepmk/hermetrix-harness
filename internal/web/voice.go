package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Voice audio is transient. Only a finalized transcript can enter the existing
// task/chat path; neither this endpoint nor its temporary files create a task.
var voiceASRSlot = make(chan struct{}, 1)

// Whisper detects one dominant language per short utterance. A small bilingual
// context helps retain Thai script around English words without translating.
const bilingualVoicePrompt = "บทสนทนาสลับภาษาไทยและ English: สวัสดีครับ Hello, ช่วยเปิด Chat และดู Workspace, Plans, API, Whisper, Bonsai."

type localWhisper struct {
	cli, model, runtimeDir string
}

func findLocalWhisper() (localWhisper, error) {
	cli := strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_CLI"))
	if cli == "" {
		if found, err := exec.LookPath("whisper-cli"); err == nil {
			cli = found
		} else if runtime.GOOS == "windows" {
			cli = `C:\Tools\whisper.cpp\build\bin\whisper-cli.exe`
		}
	}
	if info, err := os.Stat(cli); err != nil || info.IsDir() {
		return localWhisper{}, fmt.Errorf("ไม่พบ Whisper ในเครื่อง")
	}
	model := strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_MODEL"))
	if model == "" {
		model = filepath.Clean(filepath.Join(filepath.Dir(cli), "..", "..", "ggml-small.bin"))
	}
	if info, err := os.Stat(model); err != nil || info.IsDir() || info.Size() < 1 || info.Size() > 2<<30 {
		return localWhisper{}, fmt.Errorf("ไม่พบโมเดล Whisper ในเครื่อง")
	}
	w := localWhisper{cli: cli, model: model, runtimeDir: strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_RUNTIME_DIR"))}
	if w.runtimeDir == "" && runtime.GOOS == "windows" && strings.EqualFold(filepath.Clean(cli), `C:\Tools\whisper.cpp\build\bin\whisper-cli.exe`) {
		w.runtimeDir = `C:\msys64\ucrt64\bin`
	}
	return w, nil
}

func (w localWhisper) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, w.cli, args...)
	if w.runtimeDir != "" {
		cmd.Env = os.Environ()
		for i, value := range cmd.Env {
			if key, path, ok := strings.Cut(value, "="); ok && strings.EqualFold(key, "PATH") {
				cmd.Env[i] = key + "=" + w.runtimeDir + string(os.PathListSeparator) + path
				break
			}
		}
	}
	return cmd
}

func (s *Server) voiceCapabilities(w http.ResponseWriter, r *http.Request) {
	whisper, asrErr := findLocalWhisper()
	_, ffmpegErr := exec.LookPath("ffmpeg")
	_, powershellErr := findSpeechCommand()
	asrReady := asrErr == nil && ffmpegErr == nil
	ttsReady := runtime.GOOS == "windows" && powershellErr == nil
	reason := ""
	if asrErr != nil {
		reason = asrErr.Error()
	} else if ffmpegErr != nil {
		reason = "ไม่พบ FFmpeg ในเครื่อง"
	} else if !ttsReady {
		reason = "ยังไม่มีระบบพูดตอบในเครื่องนี้"
	}
	w.Header().Set("Cache-Control", "no-store")
	asrName := "Whisper ในเครื่อง"
	if asrErr == nil {
		device := "CPU"
		if strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_SERVER")) != "" {
			device = "resident"
			if strings.EqualFold(strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_DEVICE")), "CUDA") {
				device = "GPU resident"
			}
		}
		asrName = strings.TrimSuffix(strings.TrimPrefix(filepath.Base(whisper.model), "ggml-"), filepath.Ext(whisper.model)) + " · " + device
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": "local-only", "asr_ready": asrReady, "tts_ready": ttsReady,
		"asr": asrName, "tts": "Windows Speech ในเครื่อง", "reason": reason,
		"limits": map[string]any{"utterance_seconds": 20, "audio_bytes": 4 << 20, "speech_characters": 500}})
}

func findSpeechCommand() (string, error) {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if command, err := exec.LookPath(name); err == nil {
			return command, nil
		}
	}
	return "", fmt.Errorf("local speech command unavailable")
}

func (s *Server) transcribeVoice(w http.ResponseWriter, r *http.Request) {
	whisper, err := findLocalWhisper()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "ไม่พบ FFmpeg ในเครื่อง"})
		return
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "audio/webm" && mediaType != "audio/wav" && mediaType != "audio/mp4" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "ใช้เสียง WebM, WAV หรือ MP4 เท่านั้น"})
		return
	}
	select {
	case voiceASRSlot <- struct{}{}:
		defer func() { <-voiceASRSlot }()
	case <-r.Context().Done():
		return
	}
	audio, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil || len(audio) < 100 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "เสียงว่างหรือเกิน 4 MiB"})
		return
	}
	if mediaType == "audio/webm" && !bytes.HasPrefix(audio, []byte{0x1a, 0x45, 0xdf, 0xa3}) ||
		mediaType == "audio/wav" && !bytes.HasPrefix(audio, []byte("RIFF")) ||
		mediaType == "audio/mp4" && (len(audio) < 8 || !bytes.Equal(audio[4:8], []byte("ftyp"))) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "รูปแบบไฟล์เสียงไม่ตรงกับที่ระบุ"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	tempDir, err := os.MkdirTemp("", "hermetrix-voice-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "สร้างพื้นที่เสียงชั่วคราวไม่ได้"})
		return
	}
	defer os.RemoveAll(tempDir)
	ext := map[string]string{"audio/webm": ".webm", "audio/wav": ".wav", "audio/mp4": ".mp4"}[mediaType]
	inputPath := filepath.Join(tempDir, "input"+ext)
	if err := os.WriteFile(inputPath, audio, 0600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "เขียนเสียงชั่วคราวไม่ได้"})
		return
	}
	wavPath := filepath.Join(tempDir, "speech.wav")
	decode := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-protocol_whitelist", "file", "-i", inputPath,
		"-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-t", "20", "-y", wavPath)
	if output, err := decode.CombinedOutput(); err != nil {
		_ = output // Decoder details can contain local paths and are not sent to the browser.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "อ่านเสียงไม่ได้ กรุณาลองพูดใหม่"})
		return
	}
	info, err := os.Stat(wavPath)
	if err != nil || info.Size() < 1000 || info.Size() >= 640044 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "เสียงสั้นเกินไปหรือยาวเกิน 20 วินาที"})
		return
	}
	prefix := filepath.Join(tempDir, "transcript")
	command := whisper.command(ctx, "-m", whisper.model, "-f", wavPath, "-l", "auto", "--prompt", bilingualVoicePrompt, "-t", "4", "-ng", "-oj", "-of", prefix, "-np")
	if output, err := command.CombinedOutput(); err != nil {
		_ = output
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Whisper ถอดเสียงไม่สำเร็จ"})
		return
	}
	data, err := os.ReadFile(prefix + ".json")
	if err != nil || len(data) > 1<<20 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Whisper ไม่ส่งข้อความกลับมา"})
		return
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
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "ผลถอดเสียงอ่านไม่ได้"})
		return
	}
	parts := make([]string, 0, len(result.Transcription))
	for _, segment := range result.Transcription {
		if text := strings.TrimSpace(segment.Text); text != "" {
			parts = append(parts, text)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"text": strings.Join(parts, " "), "language": result.Result.Language,
		"final": true, "processor": "whisper-local"})
}

const speechScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Speech
$utterance = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:HERMETRIX_TTS_TEXT_BASE64))
if ([string]::IsNullOrWhiteSpace($utterance)) { throw 'missing speech input' }
$synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
$culture = if ($utterance -match '[\u0E00-\u0E7F]') { 'th-TH' } else { 'en-US' }
$voice = $synth.GetInstalledVoices() | Where-Object { $_.VoiceInfo.Culture.Name -eq $culture } | Select-Object -First 1
if ($voice) { $synth.SelectVoice($voice.VoiceInfo.Name) }
$synth.SetOutputToWaveFile($env:HERMETRIX_TTS_WAV_PATH)
$synth.Speak($utterance)
$synth.Dispose()
`

func (s *Server) speakVoice(w http.ResponseWriter, r *http.Request) {
	if runtime.GOOS != "windows" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "ยังไม่มีเสียงพูดตอบในเครื่องนี้"})
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if !decodeJSONLimit(w, r, &input, 4096) {
		return
	}
	utterance := strings.TrimSpace(input.Text)
	if utterance == "" || utf8.RuneCountInString(utterance) > 500 || strings.ContainsAny(utterance, "\x00\r") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ข้อความเสียงต้องมี 1–500 ตัวอักษร"})
		return
	}
	encoded := base64.StdEncoding.EncodeToString(utf16LE(speechScript))
	speechCommand, err := findSpeechCommand()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "ยังไม่มีเสียงพูดในเครื่องนี้"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	tempDir, err := os.MkdirTemp("", "hermetrix-voice-output-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "สร้างเสียงชั่วคราวไม่ได้"})
		return
	}
	defer os.RemoveAll(tempDir)
	wavPath := filepath.Join(tempDir, "speech.wav")
	cmd := exec.CommandContext(ctx, speechCommand, "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	cmd.Env = append(os.Environ(), "HERMETRIX_TTS_TEXT_BASE64="+base64.StdEncoding.EncodeToString([]byte(utterance)), "HERMETRIX_TTS_WAV_PATH="+wavPath)
	if err := cmd.Run(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "เสียงพูดในเครื่องทำงานไม่สำเร็จ"})
		return
	}
	sound, err := os.ReadFile(wavPath)
	if err != nil || len(sound) < 100 || len(sound) > 8<<20 || !bytes.HasPrefix(sound, []byte("RIFF")) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "เสียงพูดในเครื่องทำงานไม่สำเร็จ"})
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprint(len(sound)))
	_, _ = w.Write(sound)
}

func utf16LE(value string) []byte {
	units := utf16.Encode([]rune(value))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		data[i*2] = byte(unit)
		data[i*2+1] = byte(unit >> 8)
	}
	return data
}
