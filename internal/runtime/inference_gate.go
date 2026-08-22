package runtime

import (
	"context"
	"sync"
)

// InferenceGate serializes access to a local model runtime. Foreground work
// always preempts a cooperative background review and then takes the gate.
type InferenceGate struct {
	mu                sync.Mutex
	changed           chan struct{}
	foregroundActive  bool
	foregroundWaiting int
	backgroundActive  bool
	backgroundCancel  context.CancelFunc
}

func NewInferenceGate() *InferenceGate { return &InferenceGate{changed: make(chan struct{})} }

func (g *InferenceGate) RunForeground(ctx context.Context, run func(context.Context) error) error {
	g.mu.Lock()
	g.foregroundWaiting++
	g.signalLocked()
	g.mu.Unlock()
	claimed := false
	defer func() {
		if claimed {
			return
		}
		g.mu.Lock()
		g.foregroundWaiting--
		g.signalLocked()
		g.mu.Unlock()
	}()
	for {
		g.mu.Lock()
		if g.backgroundActive && g.backgroundCancel != nil {
			g.backgroundCancel()
		}
		if !g.foregroundActive && !g.backgroundActive {
			g.foregroundWaiting--
			g.foregroundActive = true
			claimed = true
			g.signalLocked()
			g.mu.Unlock()
			break
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
	defer func() {
		g.mu.Lock()
		g.foregroundActive = false
		g.signalLocked()
		g.mu.Unlock()
	}()
	return run(ctx)
}

func (g *InferenceGate) RunBackground(ctx context.Context, run func(context.Context) error) error {
	for {
		g.mu.Lock()
		if !g.foregroundActive && !g.backgroundActive && g.foregroundWaiting == 0 {
			backgroundCtx, cancel := context.WithCancel(ctx)
			g.backgroundActive = true
			g.backgroundCancel = cancel
			g.signalLocked()
			g.mu.Unlock()
			err := run(backgroundCtx)
			cancel()
			g.mu.Lock()
			g.backgroundActive = false
			g.backgroundCancel = nil
			g.signalLocked()
			g.mu.Unlock()
			return err
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (g *InferenceGate) State() (foreground, background bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.foregroundActive, g.backgroundActive
}

func (g *InferenceGate) signalLocked() {
	close(g.changed)
	g.changed = make(chan struct{})
}
