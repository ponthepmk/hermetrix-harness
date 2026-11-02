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
	"path/filepath"
	"strings"
	"time"
)

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
	if !voiceSignalPresent(pcm) {
		return "", "", nil
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
	_ = writer.WriteField("response_format", "verbose_json")
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
		Segments []struct {
			Text     string   `json:"text"`
			NoSpeech *float64 `json:"no_speech_prob"`
			LogProb  *float64 `json:"avg_logprob"`
		} `json:"segments"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return "", "", err
	}
	if result.Segments != nil {
		var accepted []string
		for _, segment := range result.Segments {
			if segment.NoSpeech != nil && (*segment.NoSpeech >= .9 || *segment.NoSpeech >= .6 && segment.LogProb != nil && *segment.LogProb < -1) {
				continue
			}
			if text := strings.TrimSpace(segment.Text); text != "" {
				accepted = append(accepted, text)
			}
		}
		return strings.Join(accepted, " "), result.Language, nil
	}
	return strings.TrimSpace(result.Text), result.Language, nil
}
