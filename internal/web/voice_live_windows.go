//go:build windows

package web

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type liveFrame struct {
	pcm []byte
	err error
}

type liveASR struct {
	epoch    int
	partial  bool
	text     string
	language string
	err      error
}

var liveVoiceSlot = make(chan struct{}, 1)

// Browser audio is ephemeral. Only a final transcript may be submitted by the
// UI through the existing, idempotent text-turn endpoint.
func (s *Server) streamBrowserVoice(w http.ResponseWriter, r *http.Request) {
	select {
	case liveVoiceSlot <- struct{}{}:
		defer func() { <-liveVoiceSlot }()
	default:
		writeJSON(w, http.StatusConflict, map[string]any{"error": "another live voice session is active"})
		return
	}
	if _, err := findLocalWhisper(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	origin := r.Header.Get("Origin")
	if origin == "" || (origin != "http://"+r.Host && origin != "https://"+r.Host) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "voice origin is not allowed"})
		return
	}
	upgrader := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096,
		CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1290)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	frames := make(chan liveFrame, 16)
	go func() {
		defer close(frames)
		var lastSeq, lastMS uint32
		var seen bool
		for {
			_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
			typ, payload, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			if typ != websocket.BinaryMessage || len(payload) != 1288 {
				return
			}
			seq, ms := binary.LittleEndian.Uint32(payload), binary.LittleEndian.Uint32(payload[4:])
			if seen && (seq != lastSeq+1 || ms <= lastMS || ms-lastMS > 500) {
				return
			}
			seen, lastSeq, lastMS = true, seq, ms
			pcm := append([]byte(nil), payload[8:]...)
			select {
			case frames <- liveFrame{pcm: pcm}:
			case <-ctx.Done():
				return
			}
		}
	}()
	send := func(value map[string]any) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		return conn.WriteJSON(value) == nil
	}
	voiceSessionBytes := make([]byte, 16)
	if _, err := rand.Read(voiceSessionBytes); err != nil {
		return
	}
	voiceSessionID := hex.EncodeToString(voiceSessionBytes)
	if !send(map[string]any{"type": "capture_started", "voice_session_id": voiceSessionID}) {
		return
	}
	results := make(chan liveASR, 4)
	var recent, captured []byte
	var recording, partialRunning bool
	var loudFrames, epoch, levelTicks int
	var lastLoud, lastPartial time.Time
	var asrCancel context.CancelFunc
	launch := func(pcm []byte, current int, partial bool) {
		processCtx, stop := context.WithTimeout(ctx, 40*time.Second)
		asrCancel = stop
		if partial {
			partialRunning = true
		}
		go func() {
			defer stop()
			value, language, err := transcribeNativePCM(processCtx, pcm)
			select {
			case results <- liveASR{epoch: current, partial: partial, text: value, language: language, err: err}:
			case <-ctx.Done():
			}
		}()
	}
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
			if result.partial {
				partialRunning = false
				if recording && result.err == nil && strings.TrimSpace(result.text) != "" {
					if !send(map[string]any{"type": "partial", "text": result.text, "language": result.language, "epoch": epoch}) {
						return
					}
				}
				continue
			}
			if result.err != nil {
				if !send(map[string]any{"type": "transcribe_error", "message": "ถอดคำพูดไม่สำเร็จ"}) {
					return
				}
			} else if strings.TrimSpace(result.text) != "" {
				if !send(map[string]any{"type": "final", "text": result.text, "language": result.language, "epoch": epoch, "voice_turn_id": voiceSessionID + ":" + strconv.Itoa(epoch)}) {
					return
				}
			} else if !send(map[string]any{"type": "no_speech"}) {
				return
			}
		case frame, open := <-frames:
			if !open || frame.err != nil {
				if asrCancel != nil {
					asrCancel()
				}
				return
			}
			now := time.Now()
			level := pcmLevel(frame.pcm)
			levelTicks++
			if levelTicks >= 5 {
				levelTicks = 0
				if !send(map[string]any{"type": "level", "rms": level}) {
					return
				}
			}
			if level > .012 {
				loudFrames++
				lastLoud = now
				if !recording && loudFrames >= 5 {
					if asrCancel != nil {
						asrCancel()
					}
					epoch++
					captured = append(make([]byte, 0, 640000), recent...)
					recording = true
					partialRunning = false
					lastPartial = now
					if !send(map[string]any{"type": "speech_start", "epoch": epoch}) {
						return
					}
				}
			} else {
				loudFrames = 0
			}
			if recording {
				captured = append(captured, frame.pcm...)
				if now.Sub(lastLoud) > 950*time.Millisecond || len(captured) >= 18*32000 {
					if asrCancel != nil {
						asrCancel()
					}
					pcm := append([]byte(nil), captured...)
					captured = nil
					recording = false
					partialRunning = false
					if !send(map[string]any{"type": "transcribing", "epoch": epoch}) {
						return
					}
					launch(pcm, epoch, false)
				} else if !partialRunning && len(captured) >= 25600 && now.Sub(lastPartial) >= 900*time.Millisecond {
					lastPartial = now
					launch(append([]byte(nil), captured...), epoch, true)
				}
			} else {
				recent = append(recent, frame.pcm...)
				if len(recent) > 6400 {
					recent = append([]byte(nil), recent[len(recent)-6400:]...)
				}
			}
		}
	}
}
