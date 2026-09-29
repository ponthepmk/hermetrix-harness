//go:build !windows

package product

import (
	"context"
	"errors"
	"time"
)

var ErrDesktopUnsupported = errors.New("native Windows observation is unavailable on this operating system")

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

func (s *Service) ListDesktopWindows(context.Context) ([]DesktopWindow, error) {
	return nil, ErrDesktopUnsupported
}

func (s *Service) ObserveDesktopWindow(context.Context, int64, int) (DesktopObservation, error) {
	return DesktopObservation{}, ErrDesktopUnsupported
}

func (s *Service) PrepareDesktopAction(context.Context, DesktopActionInput) (DesktopAction, error) {
	return DesktopAction{}, ErrDesktopUnsupported
}

func (s *Service) GetDesktopAction(context.Context, string) (DesktopAction, error) {
	return DesktopAction{}, ErrDesktopUnsupported
}

func (s *Service) ListDesktopActions(context.Context, string) ([]DesktopAction, error) {
	return []DesktopAction{}, nil
}

func (s *Service) CancelDesktopAction(context.Context, string, string) (DesktopAction, error) {
	return DesktopAction{}, ErrDesktopUnsupported
}

func (s *Service) ConfirmDesktopAction(context.Context, string, string, string, string) (DesktopAction, error) {
	return DesktopAction{}, ErrDesktopUnsupported
}

func (s *Service) ReconcileDesktopAction(context.Context, string, string, string, string, string) (DesktopAction, error) {
	return DesktopAction{}, ErrDesktopUnsupported
}
