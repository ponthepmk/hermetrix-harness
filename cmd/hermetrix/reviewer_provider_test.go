package main

import (
	"context"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
	"testing"
)

func TestExplicitBackgroundReviewerDoesNotFallBackToRemote(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := providers.NewService(db, nil)
	remote, err := service.Save(ctx, providers.SaveInput{Name: "A remote", BaseURL: "https://review.example/v1", Model: "remote", ContextWindow: 32768})
	if err != nil {
		t.Fatal(err)
	}
	local, err := service.Save(ctx, providers.SaveInput{Name: "Z local", BaseURL: "http://127.0.0.1:8088/v1", Model: "local", ContextWindow: 32768})
	if err != nil {
		t.Fatal(err)
	}
	got, err := backgroundReviewProvider(ctx, service, local.ID)
	if err != nil || got.ID != local.ID {
		t.Fatalf("explicit local reviewer=%+v err=%v", got, err)
	}
	legacy, err := backgroundReviewProvider(ctx, service, "")
	if err != nil || legacy.ID != remote.ID {
		t.Fatalf("legacy selection=%+v err=%v", legacy, err)
	}
	if _, err := backgroundReviewProvider(ctx, service, "missing-profile"); err == nil {
		t.Fatal("missing reviewer fell back to another provider")
	}
	disabled := false
	if _, err := service.Save(ctx, providers.SaveInput{ID: local.ID, Name: local.Name, BaseURL: local.BaseURL, Model: local.Model, ContextWindow: local.ContextWindow, Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := backgroundReviewProvider(ctx, service, local.ID); err == nil {
		t.Fatal("disabled reviewer fell back to remote")
	}
}
