package inference

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"hermetrix-harness/internal/store"
)

func TestLocalResourceSerializesAndPersistsUsage(t *testing.T) {
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	scheduler := New(dataStore)
	var mu sync.Mutex
	active, maximum := 0, 0
	var wg sync.WaitGroup
	for index := 0; index < 6; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, callErr := scheduler.Do(context.Background(), Request{OwnerKind: "session", OwnerID: "s1",
				ResourceKey: "local:openai:http://127.0.0.1:8080", Local: true, PromptTokens: 10, OutputTokens: 5},
				func(context.Context) (Usage, error) {
					mu.Lock()
					active++
					if active > maximum {
						maximum = active
					}
					mu.Unlock()
					time.Sleep(5 * time.Millisecond)
					mu.Lock()
					active--
					mu.Unlock()
					return Usage{PromptTokens: 8, OutputTokens: 3}, nil
				})
			if callErr != nil {
				t.Error(callErr)
			}
		}()
	}
	wg.Wait()
	if maximum != 1 {
		t.Fatalf("maximum local concurrency = %d, want 1", maximum)
	}
	var reconciled int
	if err := dataStore.DB.QueryRow(`SELECT COUNT(*) FROM inference_usage_ledger WHERE state='reconciled'`).Scan(&reconciled); err != nil {
		t.Fatal(err)
	}
	if reconciled != 6 {
		t.Fatalf("reconciled rows = %d, want 6", reconciled)
	}
}

func TestReservationRejectsBudgetBeforeDispatch(t *testing.T) {
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	scheduler := New(dataStore)
	dispatched := false
	_, err = scheduler.Do(context.Background(), Request{OwnerKind: "session", OwnerID: "limited",
		ResourceKey: "remote:p1", PromptTokens: 90, OutputTokens: 20, OwnerTokenLimit: 100},
		func(context.Context) (Usage, error) { dispatched = true; return Usage{}, nil })
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("error = %v, want budget exhausted", err)
	}
	if dispatched {
		t.Fatal("provider dispatched after reservation exceeded owner budget")
	}
}

func TestCancelledWaitReleasesReservationWithoutDispatch(t *testing.T) {
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	scheduler := New(dataStore)
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = scheduler.Do(context.Background(), Request{RequestID: "holder", ResourceKey: "local:r", Local: true,
			PromptTokens: 1, OutputTokens: 1}, func(context.Context) (Usage, error) {
			close(started)
			<-release
			return Usage{PromptTokens: 1, OutputTokens: 1}, nil
		})
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	called := false
	_, err = scheduler.Do(ctx, Request{RequestID: "cancelled", ResourceKey: "local:r", Local: true,
		PromptTokens: 1, OutputTokens: 1}, func(context.Context) (Usage, error) { called = true; return Usage{}, nil })
	close(release)
	if !(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	var state string
	if err := dataStore.DB.QueryRow(`SELECT state FROM inference_usage_ledger WHERE request_id='cancelled'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled_before_dispatch" {
		t.Fatalf("state = %s", state)
	}
}

func TestPresetIsImmutableAndValidatesEffectiveBudget(t *testing.T) {
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	preset, err := LoadPreset(context.Background(), dataStore.DB, "planner", 1)
	if err != nil {
		t.Fatal(err)
	}
	if preset.GenerationCap != 5120 || preset.ReasoningTokenCap != 3072 || preset.AnswerReserve != 2048 {
		t.Fatalf("unexpected planner preset: %+v", preset)
	}
	if err = ValidatePreset(preset, 98304, 8192, 512); err != nil {
		t.Fatal(err)
	}
	if err = ValidatePreset(preset, 98304, 4096, 512); err == nil {
		t.Fatal("planner preset passed a provider output cap below its generation reservation")
	}
	if _, err = dataStore.DB.Exec(`UPDATE inference_presets SET temperature=0.9 WHERE id='planner' AND revision=1`); err == nil {
		t.Fatal("immutable preset accepted an update")
	}
}

func TestLocalQueueIsBounded(t *testing.T) {
	scheduler := New(nil)
	scheduler.resourceActive["local:r"] = 1
	for index := 0; index < 64; index++ {
		if _, err := scheduler.enqueue(normalizeRequest(Request{RequestID: fmt.Sprintf("q-%d", index), ResourceKey: "local:r", Local: true})); err != nil {
			t.Fatalf("queue item %d: %v", index, err)
		}
	}
	if _, err := scheduler.enqueue(normalizeRequest(Request{RequestID: "overflow", ResourceKey: "local:r", Local: true})); !errors.Is(err, ErrResourceQueueFull) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestCancelledActiveLocalRequestQuarantinesResource(t *testing.T) {
	scheduler := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := scheduler.Do(ctx, Request{RequestID: "active", ResourceKey: "local:r", Local: true}, func(callCtx context.Context) (Usage, error) {
			close(started)
			<-callCtx.Done()
			return Usage{}, callCtx.Err()
		})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("active error = %v", err)
	}
	called := false
	_, err := scheduler.Do(context.Background(), Request{RequestID: "next", ResourceKey: "local:r", Local: true},
		func(context.Context) (Usage, error) { called = true; return Usage{}, nil })
	if !errors.Is(err, ErrResourceQuarantined) || called {
		t.Fatalf("quarantine err=%v called=%v", err, called)
	}
	scheduler.ReleaseQuarantine("local:r")
	if _, err = scheduler.Do(context.Background(), Request{RequestID: "after-reset", ResourceKey: "local:r", Local: true},
		func(context.Context) (Usage, error) { return Usage{}, nil }); err != nil {
		t.Fatal(err)
	}
}
