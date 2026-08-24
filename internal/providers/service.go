package providers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
)

// Revision identifies every provider field that changes request semantics or
// qualification validity. Credential values remain outside the profile and
// therefore cannot enter this digest.
func Revision(profile Profile) string {
	payload := struct {
		AdapterKind     string `json:"adapter_kind"`
		BaseURL         string `json:"base_url"`
		Model           string `json:"model"`
		APIKeyEnv       string `json:"api_key_env"`
		ContextWindow   int    `json:"context_window"`
		ContextEvidence string `json:"context_evidence"`
		MaxOutputTokens int    `json:"max_output_tokens"`
	}{profile.AdapterKind, profile.BaseURL, profile.Model, profile.APIKeyEnv, profile.ContextWindow,
		profile.ContextEvidence, profile.MaxOutputTokens}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return "provider-" + hex.EncodeToString(sum[:8])
}

var envNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,126}$`)

type Service struct {
	store   *store.Store
	adapter *OpenAIAdapter
}

func NewService(dataStore *store.Store, adapter *OpenAIAdapter) *Service {
	if adapter == nil {
		adapter = NewOpenAIAdapter(nil)
	}
	return &Service{store: dataStore, adapter: adapter}
}

func (s *Service) Save(ctx context.Context, input SaveInput) (Profile, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.AdapterKind = strings.TrimSpace(input.AdapterKind)
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Model = strings.TrimSpace(input.Model)
	input.APIKeyEnv = strings.TrimSpace(input.APIKeyEnv)
	if input.AdapterKind == "" {
		input.AdapterKind = AdapterOpenAICompatible
	}
	if input.ContextEvidence == "" {
		input.ContextEvidence = "declared"
	}
	if input.MaxOutputTokens == 0 {
		input.MaxOutputTokens = 4096
	}
	if err := validateInput(input); err != nil {
		return Profile{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	if input.ID == "" {
		input.ID = identity.New("provider")
		_, err := s.store.DB.ExecContext(ctx, `INSERT INTO provider_profiles
      (id,name,adapter_kind,base_url,model,api_key_env,context_window,context_evidence,max_output_tokens,enabled,created_at,updated_at)
      VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, input.ID, input.Name, input.AdapterKind, input.BaseURL, input.Model,
			input.APIKeyEnv, input.ContextWindow, input.ContextEvidence, input.MaxOutputTokens, boolInt(enabled), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return Profile{}, fmt.Errorf("create provider profile: %w", err)
		}
	} else {
		result, err := s.store.DB.ExecContext(ctx, `UPDATE provider_profiles SET
      name=?,adapter_kind=?,base_url=?,model=?,api_key_env=?,context_window=?,context_evidence=?,max_output_tokens=?,enabled=?,updated_at=? WHERE id=?`,
			input.Name, input.AdapterKind, input.BaseURL, input.Model, input.APIKeyEnv, input.ContextWindow,
			input.ContextEvidence, input.MaxOutputTokens, boolInt(enabled), now.Format(time.RFC3339Nano), input.ID)
		if err != nil {
			return Profile{}, fmt.Errorf("update provider profile: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return Profile{}, sql.ErrNoRows
		}
	}
	return s.Get(ctx, input.ID)
}

// EnsureByName creates or updates a startup-supplied provider without ever
// accepting the credential value. Only the environment variable name persists.
func (s *Service) EnsureByName(ctx context.Context, input SaveInput) (Profile, error) {
	var id string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM provider_profiles WHERE name=?`, strings.TrimSpace(input.Name)).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Profile{}, err
	}
	if err == nil {
		input.ID = id
	}
	return s.Save(ctx, input)
}

func (s *Service) List(ctx context.Context) ([]Profile, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,name,adapter_kind,base_url,model,api_key_env,context_window,
    context_evidence,max_output_tokens,enabled,reasoning_ratio,reasoning_sample,created_at,updated_at
    FROM provider_profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list provider profiles: %w", err)
	}
	defer rows.Close()
	items := []Profile{}
	for rows.Next() {
		item, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// FirstEnabled returns a usable provider for work that has no session behind
// it, such as background review. It prefers the oldest enabled profile so the
// choice does not move when profiles are edited.
func (s *Service) FirstEnabled(ctx context.Context) (Profile, error) {
	items, err := s.List(ctx)
	if err != nil {
		return Profile{}, err
	}
	for _, item := range items {
		if item.Enabled && strings.TrimSpace(item.APIKeyEnv) != "" {
			return item, nil
		}
	}
	return Profile{}, fmt.Errorf("no enabled provider profile is configured")
}

func (s *Service) Get(ctx context.Context, id string) (Profile, error) {
	row := s.store.DB.QueryRowContext(ctx, `SELECT id,name,adapter_kind,base_url,model,api_key_env,context_window,
    context_evidence,max_output_tokens,enabled,reasoning_ratio,reasoning_sample,created_at,updated_at
    FROM provider_profiles WHERE id=?`, id)
	return scanProfile(row)
}

func (s *Service) StreamChat(ctx context.Context, profile Profile, request ChatRequest, emit func(Delta) error) (Completion, error) {
	if !profile.Enabled {
		return Completion{}, fmt.Errorf("provider profile is disabled")
	}
	key, err := credential(profile)
	if err != nil {
		return Completion{}, err
	}
	return s.adapter.StreamChat(ctx, profile, key, request, emit)
}

func (s *Service) Test(ctx context.Context, id string) (TestResult, error) {
	profile, err := s.Get(ctx, id)
	if err != nil {
		return TestResult{}, err
	}
	start := time.Now()
	temperature := 0.0
	completion, err := s.StreamChat(ctx, profile, ChatRequest{Messages: []Message{
		{Role: "system", Content: "You are a connectivity probe. Follow the user's requested output exactly."},
		{Role: "user", Content: "/no_think Reply with exactly: HERMETRIX_OK"},
	}, Temperature: &temperature, MaxTokens: 256}, nil)
	if err != nil {
		return TestResult{}, err
	}
	sample := strings.TrimSpace(completion.Content)
	if sample == "" && completion.Reasoning != "" {
		sample = "connected; model returned reasoning only within the probe budget"
	}
	if len([]rune(sample)) > 160 {
		sample = string([]rune(sample)[:160]) + "…"
	}
	return TestResult{ProviderID: profile.ID, Model: profile.Model, LatencyMS: time.Since(start).Milliseconds(),
		Sample: sample, FinishReason: completion.FinishReason, Usage: completion.Usage}, nil
}

type scanner interface{ Scan(...any) error }

func scanProfile(row scanner) (Profile, error) {
	var item Profile
	var enabled int
	var created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.AdapterKind, &item.BaseURL, &item.Model, &item.APIKeyEnv,
		&item.ContextWindow, &item.ContextEvidence, &item.MaxOutputTokens, &enabled,
		&item.ReasoningRatio, &item.ReasoningSample, &created, &updated); err != nil {
		return Profile{}, err
	}
	item.Enabled = enabled != 0
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	_, item.CredentialReady = os.LookupEnv(item.APIKeyEnv)
	if item.APIKeyEnv == "" {
		item.CredentialReady = true
	}
	item.Revision = Revision(item)
	return item, nil
}

func validateInput(input SaveInput) error {
	if input.Name == "" || len(input.Name) > 80 {
		return fmt.Errorf("provider name is required and must be at most 80 characters")
	}
	if input.AdapterKind != AdapterOpenAICompatible {
		return fmt.Errorf("unsupported provider adapter %q", input.AdapterKind)
	}
	if input.Model == "" || len(input.Model) > 240 {
		return fmt.Errorf("provider model is required and must be at most 240 characters")
	}
	parsed, err := url.Parse(input.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("provider base URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return fmt.Errorf("provider base URL must use https; http is allowed only on loopback")
	}
	if input.APIKeyEnv != "" && !envNamePattern.MatchString(input.APIKeyEnv) {
		return fmt.Errorf("API key environment variable must use uppercase letters, digits, and underscores")
	}
	if input.ContextWindow < 4096 || input.ContextWindow > 2_097_152 {
		return fmt.Errorf("context window must be between 4k and 2M tokens")
	}
	if input.ContextEvidence != "declared" && input.ContextEvidence != "runtime_probe" && input.ContextEvidence != "qualified" {
		return fmt.Errorf("context evidence must be declared, runtime_probe, or qualified")
	}
	if input.MaxOutputTokens < 128 || input.MaxOutputTokens > input.ContextWindow/2 {
		return fmt.Errorf("max output tokens must be between 128 and half the context window")
	}
	return nil
}

func credential(profile Profile) (string, error) {
	if profile.APIKeyEnv == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(profile.APIKeyEnv)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("credential environment variable %s is not set", profile.APIKeyEnv)
	}
	return value, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// ObserveReasoning folds one completion into the running share of output this
// model spends thinking. Kept as a simple running mean: the value only has to
// be good enough to tell "answers get the whole budget" from "answers get a
// tenth of it", and a mean over every completion is harder to skew than a
// window.
//
// Callers observe after every completion; sessions freeze the value at open so
// a budget cannot shift under a live conversation.
func (s *Service) ObserveReasoning(ctx context.Context, providerID string, reasoningTokens, completionTokens int) error {
	if completionTokens <= 0 || reasoningTokens < 0 || strings.TrimSpace(providerID) == "" {
		return nil
	}
	share := float64(reasoningTokens) / float64(completionTokens)
	if share > 1 {
		share = 1
	}
	_, err := s.store.DB.ExecContext(ctx, `UPDATE provider_profiles
		SET reasoning_ratio = ((reasoning_ratio * reasoning_sample) + ?) / (reasoning_sample + 1),
		    reasoning_sample = reasoning_sample + 1
		WHERE id=?`, share, providerID)
	return err
}
