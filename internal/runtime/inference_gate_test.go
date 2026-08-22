package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestForegroundPreemptsBackground(t *testing.T) {
	gate := NewInferenceGate()
	backgroundStarted := make(chan struct{})
	backgroundDone := make(chan error, 1)
	go func() {
		backgroundDone <- gate.RunBackground(context.Background(), func(ctx context.Context) error {
			close(backgroundStarted)
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	<-backgroundStarted
	foregroundRan := false
	if err := gate.RunForeground(context.Background(), func(context.Context) error { foregroundRan = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !foregroundRan {
		t.Fatal("foreground did not acquire the gate")
	}
	select {
	case err := <-backgroundDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("background error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("background was not preempted")
	}
}

func TestWaitingForegroundHasPriorityOverNewBackground(t *testing.T) {
	gate := NewInferenceGate()
	foregroundRelease := make(chan struct{})
	foregroundStarted := make(chan struct{})
	foregroundDone := make(chan struct{})
	go func() {
		_ = gate.RunForeground(context.Background(), func(context.Context) error {
			close(foregroundStarted)
			<-foregroundRelease
			return nil
		})
		close(foregroundDone)
	}()
	<-foregroundStarted

	backgroundStarted := make(chan struct{})
	backgroundDone := make(chan struct{})
	go func() {
		_ = gate.RunBackground(context.Background(), func(context.Context) error {
			close(backgroundStarted)
			return nil
		})
		close(backgroundDone)
	}()

	select {
	case <-backgroundStarted:
		t.Fatal("background started while foreground inference was active")
	case <-time.After(20 * time.Millisecond):
	}
	close(foregroundRelease)
	<-foregroundDone
	select {
	case <-backgroundDone:
	case <-time.After(time.Second):
		t.Fatal("background did not resume after foreground completed")
	}
}

func TestForegroundCallsAreSerialized(t *testing.T) {
	gate := NewInferenceGate()
	var mu sync.Mutex
	active, maxActive := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = gate.RunForeground(context.Background(), func(context.Context) error {
				mu.Lock()
				active++
				if active > maxActive {
					maxActive = active
				}
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				active--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	if maxActive != 1 {
		t.Fatalf("max concurrent foreground calls = %d", maxActive)
	}
}
