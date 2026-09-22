package qualification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/localmodel"
	"hermetrix-harness/internal/providers"
)

type RuntimeFingerprint struct {
	ID                string         `json:"id"`
	Digest            string         `json:"digest"`
	RuntimeKind       string         `json:"runtime_kind"`
	EndpointIdentity  string         `json:"endpoint_identity"`
	BuildRevision     string         `json:"build_revision,omitempty"`
	ModelID           string         `json:"model_id"`
	ModelChecksums    []string       `json:"model_checksums"`
	ProjectorChecksum string         `json:"projector_checksum,omitempty"`
	ChatTemplateHash  string         `json:"chat_template_hash,omitempty"`
	TokenizerRevision string         `json:"tokenizer_revision,omitempty"`
	KVSettings        map[string]any `json:"kv_settings"`
	ContextCapacity   int            `json:"context_capacity"`
	DeviceMapping     []string       `json:"device_mapping"`
	ConfigDigest      string         `json:"config_digest"`
	EvidenceQuality   string         `json:"evidence_quality"`
	CreatedAt         time.Time      `json:"created_at"`
}

func (s *Service) recordRuntimeFingerprint(ctx context.Context, profile providers.Profile, probe *localmodel.Result,
	input RuntimeIdentityInput) (RuntimeFingerprint, error) {
	runtimeKind, endpoint, capacity := "remote", profile.BaseURL, profile.ContextWindow
	if probe != nil {
		runtimeKind, endpoint, capacity = probe.Runtime, probe.Endpoint, probe.AllocatedContext
	}
	checksums := normalizedStrings(input.ModelChecksums)
	devices := normalizedStrings(input.DeviceMapping)
	kv := input.KVSettings
	if kv == nil {
		kv = map[string]any{}
	}
	configDigest := strings.TrimSpace(input.ConfigDigest)
	if configDigest == "" {
		configDigest = digestJSON(struct {
			Runtime  string         `json:"runtime"`
			Endpoint string         `json:"endpoint"`
			KV       map[string]any `json:"kv"`
			Devices  []string       `json:"devices"`
			Capacity int            `json:"capacity"`
		}{runtimeKind, endpoint, kv, devices, capacity})
	}
	fingerprint := RuntimeFingerprint{RuntimeKind: runtimeKind, EndpointIdentity: endpoint,
		BuildRevision: strings.TrimSpace(input.BuildRevision), ModelID: profile.Model, ModelChecksums: checksums,
		ProjectorChecksum: strings.TrimSpace(input.ProjectorChecksum), ChatTemplateHash: strings.TrimSpace(input.ChatTemplateHash),
		TokenizerRevision: strings.TrimSpace(input.TokenizerRevision), KVSettings: kv, ContextCapacity: capacity,
		DeviceMapping: devices, ConfigDigest: configDigest, EvidenceQuality: "partial", CreatedAt: time.Now().UTC()}
	if fingerprint.BuildRevision != "" && len(checksums) > 0 && fingerprint.ChatTemplateHash != "" &&
		fingerprint.TokenizerRevision != "" && capacity > 0 && len(devices) > 0 {
		fingerprint.EvidenceQuality = "exact"
	}
	fingerprint.Digest = digestJSON(struct {
		RuntimeKind       string         `json:"runtime_kind"`
		EndpointIdentity  string         `json:"endpoint_identity"`
		BuildRevision     string         `json:"build_revision"`
		ModelID           string         `json:"model_id"`
		ModelChecksums    []string       `json:"model_checksums"`
		ProjectorChecksum string         `json:"projector_checksum"`
		ChatTemplateHash  string         `json:"chat_template_hash"`
		TokenizerRevision string         `json:"tokenizer_revision"`
		KVSettings        map[string]any `json:"kv_settings"`
		ContextCapacity   int            `json:"context_capacity"`
		DeviceMapping     []string       `json:"device_mapping"`
		ConfigDigest      string         `json:"config_digest"`
	}{fingerprint.RuntimeKind, fingerprint.EndpointIdentity, fingerprint.BuildRevision, fingerprint.ModelID, fingerprint.ModelChecksums,
		fingerprint.ProjectorChecksum, fingerprint.ChatTemplateHash, fingerprint.TokenizerRevision, fingerprint.KVSettings,
		fingerprint.ContextCapacity, fingerprint.DeviceMapping, fingerprint.ConfigDigest})
	modelJSON, _ := json.Marshal(fingerprint.ModelChecksums)
	kvJSON, _ := json.Marshal(fingerprint.KVSettings)
	deviceJSON, _ := json.Marshal(fingerprint.DeviceMapping)
	var existing string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM runtime_fingerprints WHERE digest=?`, fingerprint.Digest).Scan(&existing)
	if err == nil {
		fingerprint.ID = existing
		return fingerprint, nil
	}
	fingerprint.ID = identity.New("runtime")
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO runtime_fingerprints(id,digest,runtime_kind,endpoint_identity,build_revision,
		model_id,model_checksums_json,projector_checksum,chat_template_hash,tokenizer_revision,kv_settings_json,context_capacity,
		device_mapping_json,config_digest,evidence_quality,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, fingerprint.ID,
		fingerprint.Digest, fingerprint.RuntimeKind, fingerprint.EndpointIdentity, fingerprint.BuildRevision, fingerprint.ModelID,
		string(modelJSON), fingerprint.ProjectorChecksum, fingerprint.ChatTemplateHash, fingerprint.TokenizerRevision, string(kvJSON),
		fingerprint.ContextCapacity, string(deviceJSON), fingerprint.ConfigDigest, fingerprint.EvidenceQuality, formatTime(fingerprint.CreatedAt))
	if err != nil {
		return RuntimeFingerprint{}, fmt.Errorf("record runtime fingerprint: %w", err)
	}
	return fingerprint, nil
}

func normalizedStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func digestJSON(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
