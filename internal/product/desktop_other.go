//go:build !windows

package product

import (
	"context"
	"errors"
)

var ErrDesktopUnsupported = errors.New("native Windows observation is unavailable on this operating system")

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
