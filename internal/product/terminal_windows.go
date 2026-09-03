//go:build windows

package product

import (
	"context"
	"fmt"
)

type terminalRuntime struct{}

func (s *Service) StartTerminal(context.Context, StartTerminalInput) (TerminalSession, error) {
	return TerminalSession{}, fmt.Errorf("interactive PTY is not available in this Windows build yet")
}
func (s *Service) WriteTerminal(context.Context, string, string) error {
	return fmt.Errorf("interactive PTY is unavailable")
}
func (s *Service) ResizeTerminal(context.Context, string, uint16, uint16) error {
	return fmt.Errorf("interactive PTY is unavailable")
}
func (s *Service) CloseTerminal(context.Context, string) (TerminalSession, error) {
	return TerminalSession{}, fmt.Errorf("interactive PTY is unavailable")
}
func (s *Service) TerminalOutput(context.Context, string, int64) (TerminalOutput, error) {
	return TerminalOutput{}, fmt.Errorf("interactive PTY is unavailable")
}
func (s *Service) ListTerminals(context.Context, int) ([]TerminalSession, error) {
	return []TerminalSession{}, nil
}
func (s *Service) GetTerminal(context.Context, string) (TerminalSession, error) {
	return TerminalSession{}, fmt.Errorf("interactive PTY is unavailable")
}
func (s *Service) closeTerminals() {}
