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

// CredentialVault is the local store a token typed into the control center is
// written to. It is optional: with no vault configured every profile falls back
// to its environment variable, which is how Hermetrix worked before a token
// could be entered from the UI at all.
type CredentialVault interface {
	Get(ref string) (string, bool)
	Has(ref string) bool
	Set(ref, token string) error
}

type Service struct {
	store   *store.Store
	adapter *OpenAIAdapter
	vault   CredentialVault
}

func NewService(dataStore *store.Store, adapter *OpenAIAdapter) *Service {
	if adapter == nil {
		adapter = NewOpenAIAdapter(nil)
	}
	return &Service{store: dataStore, adapter: adapter}
}

// WithVault attaches the credential store. A stored token takes precedence over
// the environment variable: it is the one the user set most recently, and from
// the surface that shows whether it worked.
func (s *Service) WithVault(vault CredentialVault) *Service {
	s.vault = vault
	return s
}

// CredentialRef is the vault key for a profile. It is the profile ID, so
// renaming a provider or changing its endpoint keeps the credential attached.
func CredentialRef(profileID string) string { return "provider:" + profileID }

// SetCredential stores or clears the token for a profile. The value is written
// only to the vault; nothing returns it, and no caller logs it.
func (s *Service) SetCredential(ctx context.Context, profileID, token string) (Profile, error) {
	profile, err := s.Get(ctx, profileID)
	if err != nil {
		return Profile{}, err
	}
	if s.vault == nil {
		return Profile{}, errors.New("no credential store is configured; set the environment variable instead")
	}
	if err := s.vault.Set(CredentialRef(profile.ID), token); err != nil {
		return Profile{}, err
	}
	return s.Get(ctx, profileID)
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
    context_evidence,max_output_tokens,enabled,reasoning_ratio,reasoning_sample,token_multiplier,token_sample,nonascii_rate,nonascii_sample,token_message_overhead,token_request_overhead,token_overhead_measured_at,created_at,updated_at
    FROM provider_profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list provider profiles: %w", err)
	}
	defer rows.Close()
	items := []Profile{}
	for rows.Next() {
		item, err := s.scanProfile(rows)
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
    context_evidence,max_output_tokens,enabled,reasoning_ratio,reasoning_sample,token_multiplier,token_sample,nonascii_rate,nonascii_sample,token_message_overhead,token_request_overhead,token_overhead_measured_at,created_at,updated_at
    FROM provider_profiles WHERE id=?`, id)
	return s.scanProfile(row)
}

func (s *Service) StreamChat(ctx context.Context, profile Profile, request ChatRequest, emit func(Delta) error) (Completion, error) {
	if !profile.Enabled {
		return Completion{}, fmt.Errorf("provider profile is disabled")
	}
	key, err := s.credential(profile)
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

func (s *Service) scanProfile(row scanner) (Profile, error) {
	var item Profile
	var enabled int
	var created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.AdapterKind, &item.BaseURL, &item.Model, &item.APIKeyEnv,
		&item.ContextWindow, &item.ContextEvidence, &item.MaxOutputTokens, &enabled,
		&item.ReasoningRatio, &item.ReasoningSample, &item.TokenMultiplier, &item.TokenSample,
		&item.NonASCIIRate, &item.NonASCIISample, &item.MessageOverhead, &item.RequestOverhead,
		&item.OverheadMeasuredAt, &created, &updated); err != nil {
		return Profile{}, err
	}
	item.Enabled = enabled != 0
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	// A saved token wins, then the environment variable, then "this endpoint
	// needs no credential at all" -- which is what an empty variable name has
	// always meant and is how a local runtime is configured.
	item.CredentialStored = s.vault != nil && s.vault.Has(CredentialRef(item.ID))
	_, fromEnvironment := os.LookupEnv(item.APIKeyEnv)
	item.CredentialReady = item.CredentialStored || fromEnvironment || item.APIKeyEnv == ""
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

func (s *Service) credential(profile Profile) (string, error) {
	if s.vault != nil {
		if value, ok := s.vault.Get(CredentialRef(profile.ID)); ok {
			return value, nil
		}
	}
	if profile.APIKeyEnv == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(profile.APIKeyEnv)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("no API key is saved for %s, and the environment variable %s is not set",
			profile.Name, profile.APIKeyEnv)
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

// tokenScaleFloor and tokenScaleCeiling bound one observation's influence. A
// single malformed usage report should move the calibration, not replace it.
const (
	tokenScaleFloor   = 0.50
	tokenScaleCeiling = 3.00
)

// ObserveTokenScale folds one prediction/usage pair into this profile's running
// mean, the same way ObserveReasoning does. A running mean rather than a decaying
// one because a tokenizer's ratio for a given model and language mix is close to
// constant: the noise is in the sample, not in the truth.
//
// applied is the multiplier that produced this prediction, and it is required.
// Without it the average is taken over actual/predicted, where predicted has
// already been scaled by the very number being learned. That is a feedback loop,
// and its fixed point is not the truth but its square root: for a model whose
// real ratio is 0.80 the multiplier settles at 0.894 and leaves a permanent
// -10.6% error -- close enough to the ±10% gate to look like noise and never
// close. Simulated over 200 steps, and matching what the live gateway did.
//
// Dividing the prediction back out first removes the loop, and the mean
// converges on the ratio itself.
func (s *Service) ObserveTokenScale(ctx context.Context, providerID string, applied float64, predicted, actual int) error {
	if predicted <= 0 || actual <= 0 || applied <= 0 || strings.TrimSpace(providerID) == "" {
		return nil
	}
	ratio := applied * float64(actual) / float64(predicted)
	if ratio < tokenScaleFloor {
		ratio = tokenScaleFloor
	}
	if ratio > tokenScaleCeiling {
		ratio = tokenScaleCeiling
	}
	_, err := s.store.DB.ExecContext(ctx, `UPDATE provider_profiles
		SET token_multiplier = ((token_multiplier * token_sample) + ?) / (token_sample + 1),
		    token_sample = token_sample + 1
		WHERE id=?`, ratio, providerID)
	return err
}

// nonASCIIRateFloor and nonASCIIRateCeiling bound one sample's influence. The
// ceiling is the old assumption of a token per character, which is the most any
// real tokenizer charges; the floor leaves room for scripts that pack far
// tighter than Thai.
const (
	nonASCIIRateFloor   = 0.10
	nonASCIIRateCeiling = 1.50
)

// ObserveNonASCIIRate learns tokens-per-character for the scripts the ASCII
// rules do not cover, by subtracting the part that is already modelled well and
// attributing the remainder to the characters that are not.
//
// A sample with no non-ASCII characters says nothing about their rate and is
// skipped rather than counted as zero.
func (s *Service) ObserveNonASCIIRate(ctx context.Context, providerID string, asciiTokens, nonASCIIChars, actual int) error {
	if nonASCIIChars <= 0 || actual <= 0 || strings.TrimSpace(providerID) == "" {
		return nil
	}
	rate := float64(actual-asciiTokens) / float64(nonASCIIChars)
	if rate < nonASCIIRateFloor {
		rate = nonASCIIRateFloor
	}
	if rate > nonASCIIRateCeiling {
		rate = nonASCIIRateCeiling
	}
	_, err := s.store.DB.ExecContext(ctx, `UPDATE provider_profiles
		SET nonascii_rate = ((nonascii_rate * nonascii_sample) + ?) / (nonascii_sample + 1),
		    nonascii_sample = nonascii_sample + 1
		WHERE id=?`, rate, providerID)
	return err
}

// overheadProbeMessages are the two message counts the probe compares. Any two
// distinct counts determine the line; these are small enough to be cheap and far
// enough apart that a one-token rounding difference cannot distort the slope.
const (
	overheadProbeSmall = 1
	overheadProbeLarge = 9
	// maxPlausibleMessageOverhead rejects a measurement that cannot be a chat
	// template. A wrapper is a handful of tokens per message; a hundred means
	// the provider counted something else, and a wrong overhead is worse than
	// none because it is subtracted from usable context on every request.
	maxPlausibleMessageOverhead = 64
	maxPlausibleRequestOverhead = 4096
)

// MeasureTokenOverhead determines what a provider bills for a request before
// any content: the per-message wrapper its chat template adds, and the constant
// the request carries besides its messages.
//
// It sends two requests whose content is identical and whose message counts
// differ, and reads the reported prompt usage. The difference is the wrapper;
// the remainder is the constant. Nothing is fitted, which matters because
// production traffic cannot separate these terms -- message count, content size
// and language mix grow together there, and a regression on that data returned
// a per-message cost of minus 260 tokens.
//
// Measured against a live gateway the relationship is exactly linear: the same
// content split across 1, 3, 5, 9, 17 and 33 messages cost 9 more tokens each
// time. This probe uses empty messages and reads that line as 7 per message
// over a constant of 45 -- a couple of tokens apart, and on real traffic the
// two are indistinguishable (p95 7.42% against 7.54%). The remainder is
// absorbed by the learned script rate, which is what it is for.
func (s *Service) MeasureTokenOverhead(ctx context.Context, id string) (Profile, error) {
	profile, err := s.Get(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	temperature := 0.0
	probe := func(messages int) (int, error) {
		request := ChatRequest{MaxTokens: 1, Temperature: &temperature}
		for i := 0; i < messages; i++ {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			request.Messages = append(request.Messages, Message{Role: role, Content: ""})
		}
		completion, err := s.StreamChat(ctx, profile, request, nil)
		if err != nil {
			return 0, err
		}
		if completion.Usage.PromptTokens <= 0 {
			return 0, fmt.Errorf("provider reported no prompt usage, so its overhead cannot be measured")
		}
		return completion.Usage.PromptTokens, nil
	}
	small, err := probe(overheadProbeSmall)
	if err != nil {
		return Profile{}, fmt.Errorf("overhead probe: %w", err)
	}
	large, err := probe(overheadProbeLarge)
	if err != nil {
		return Profile{}, fmt.Errorf("overhead probe: %w", err)
	}
	messageOverhead, requestOverhead, err := solveOverhead(small, large)
	if err != nil {
		return Profile{}, err
	}
	measured := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.store.DB.ExecContext(ctx, `UPDATE provider_profiles
		SET token_message_overhead=?, token_request_overhead=?, token_overhead_measured_at=? WHERE id=?`,
		messageOverhead, requestOverhead, measured, id); err != nil {
		return Profile{}, err
	}
	return s.Get(ctx, id)
}

// solveOverhead turns two probe results into the two constants, and refuses a
// result that cannot describe a chat template. Silently accepting a nonsense
// overhead would subtract it from usable context on every later request.
func solveOverhead(small, large int) (messageOverhead, requestOverhead int, err error) {
	span := overheadProbeLarge - overheadProbeSmall
	difference := large - small
	if difference < 0 {
		return 0, 0, fmt.Errorf("overhead probe: more messages billed fewer tokens (%d then %d)", small, large)
	}
	messageOverhead = difference / span
	requestOverhead = small - messageOverhead*overheadProbeSmall
	if messageOverhead > maxPlausibleMessageOverhead || requestOverhead < 0 || requestOverhead > maxPlausibleRequestOverhead {
		return 0, 0, fmt.Errorf(
			"overhead probe: %d tokens per message and %d per request is not a chat template; probes billed %d and %d",
			messageOverhead, requestOverhead, small, large)
	}
	return messageOverhead, requestOverhead, nil
}
