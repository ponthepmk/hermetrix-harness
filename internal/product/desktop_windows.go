//go:build windows

package product

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

//go:embed desktop_probe_windows.ps1
var desktopProbeScript string

type DesktopBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type DesktopWindow struct {
	Handle    int64         `json:"handle"`
	ProcessID int           `json:"process_id"`
	Title     string        `json:"title"`
	Bounds    DesktopBounds `json:"bounds"`
}

type DesktopElement struct {
	Ref          int           `json:"ref"`
	Handle       int64         `json:"handle"`
	Name         string        `json:"name"`
	AutomationID string        `json:"automation_id"`
	ControlType  string        `json:"control_type"`
	NativeClass  string        `json:"native_class,omitempty"`
	Value        string        `json:"value,omitempty"`
	IsPassword   bool          `json:"is_password"`
	Enabled      bool          `json:"enabled"`
	Bounds       DesktopBounds `json:"bounds"`
}

type DesktopObservation struct {
	Window               DesktopWindow    `json:"window"`
	Elements             []DesktopElement `json:"elements"`
	ScreenshotArtifactID string           `json:"screenshot_artifact_id,omitempty"`
	ScreenshotHash       string           `json:"screenshot_hash,omitempty"`
	ScreenshotError      string           `json:"screenshot_error,omitempty"`
	ObservedAt           time.Time        `json:"observed_at"`
	ObservationHash      string           `json:"observation_hash"`
}

func runDesktopProbe(ctx context.Context, request any, result any) error {
	encoded := utf16.Encode([]rune(desktopProbeScript))
	bytes := make([]byte, len(encoded)*2)
	for index, value := range encoded {
		binary.LittleEndian.PutUint16(bytes[index*2:], value)
	}
	input, _ := json.Marshal(request)
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(bytes))
	command.Stdin = strings.NewReader(string(input))
	output, err := command.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("Windows observation unavailable: %s: %w", strings.TrimSpace(string(failure.Stderr)), err)
		}
		return fmt.Errorf("Windows observation unavailable: %w", err)
	}
	if err := json.Unmarshal(output, result); err != nil {
		return fmt.Errorf("decode Windows observation: %w", err)
	}
	return nil
}

func (s *Service) ListDesktopWindows(ctx context.Context) ([]DesktopWindow, error) {
	var result struct {
		Windows []DesktopWindow `json:"windows"`
	}
	if err := runDesktopProbe(ctx, map[string]any{"mode": "list"}, &result); err != nil {
		return nil, err
	}
	return result.Windows, nil
}

func (s *Service) ObserveDesktopWindow(ctx context.Context, handle int64, processID int) (DesktopObservation, error) {
	if handle == 0 || processID < 1 {
		return DesktopObservation{}, fmt.Errorf("window handle and process id are required")
	}
	tmp, err := os.CreateTemp("", "hermetrix-desktop-*.png")
	if err != nil {
		return DesktopObservation{}, err
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)
	var raw struct {
		Window          DesktopWindow    `json:"window"`
		Elements        []DesktopElement `json:"elements"`
		ScreenshotPath  string           `json:"screenshot_path"`
		ScreenshotError string           `json:"screenshot_error"`
	}
	if err := runDesktopProbe(ctx, map[string]any{"mode": "observe", "handle": handle, "process_id": processID, "screenshot_path": path}, &raw); err != nil {
		return DesktopObservation{}, err
	}
	if raw.Window.Handle != handle || raw.Window.ProcessID != processID {
		return DesktopObservation{}, fmt.Errorf("window identity changed")
	}
	result := DesktopObservation{Window: raw.Window, Elements: raw.Elements, ScreenshotError: raw.ScreenshotError, ObservedAt: time.Now().UTC()}
	if raw.ScreenshotPath != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			result.ScreenshotError = err.Error()
		} else {
			artifact, err := s.UploadMedia(ctx, MediaUploadInput{Name: "desktop-" + strconv.FormatInt(handle, 10) + ".png", MIMEType: "image/png", Data: data})
			if err != nil {
				result.ScreenshotError = err.Error()
			} else {
				result.ScreenshotArtifactID = artifact.ID
				result.ScreenshotHash = artifact.Checksum
			}
		}
	}
	result.ObservationHash = desktopObservationHash(result)
	return result, nil
}

func desktopObservationHash(observation DesktopObservation) string {
	body, _ := json.Marshal(struct {
		Window         DesktopWindow
		Elements       []DesktopElement
		ScreenshotHash string
	}{observation.Window, observation.Elements, observation.ScreenshotHash})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
