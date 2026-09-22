package qualification

import (
	"context"
	"testing"

	"hermetrix-harness/internal/localmodel"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

func TestRuntimeFingerprintUsesEvidenceAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	service := &Service{store: dataStore}
	profile := providers.Profile{Model: "model-name-is-not-a-checksum", BaseURL: "http://127.0.0.1:8080/v1", ContextWindow: 98304}
	probe := &localmodel.Result{Runtime: "llamacpp", Endpoint: profile.BaseURL, Model: profile.Model, AllocatedContext: 98304, Verified: true}
	input := RuntimeIdentityInput{BuildRevision: "commit-1", ModelChecksums: []string{"sha256:model"},
		ChatTemplateHash: "sha256:template", TokenizerRevision: "tok-1", DeviceMapping: []string{"gpu-uuid-1"},
		KVSettings: map[string]any{"k": "q8_0", "v": "q8_0"}}
	first, err := service.recordRuntimeFingerprint(ctx, profile, probe, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.recordRuntimeFingerprint(ctx, profile, probe, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.EvidenceQuality != "exact" || len(first.Digest) != 64 {
		t.Fatalf("fingerprints=%+v %+v", first, second)
	}
	unknown, err := service.recordRuntimeFingerprint(ctx, profile, probe, RuntimeIdentityInput{})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.EvidenceQuality != "partial" || len(unknown.ModelChecksums) != 0 {
		t.Fatalf("unknown evidence was fabricated: %+v", unknown)
	}
}
