// Package discordbridge connects a configured local workspace to allowlisted
// Discord slash commands. It opens only outbound connections; Discord never
// receives a public HTTP endpoint into the local application.
package discordbridge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/store"
)

const tokenRef = "discord:bot"

var snowflakePattern = regexp.MustCompile(`^[0-9]{17,20}$`)
var ErrInvalidConfig = errors.New("invalid Discord configuration")
var ErrForbidden = errors.New("Discord access forbidden")

type Config struct {
	Enabled        bool     `json:"enabled"`
	ApplicationID  string   `json:"application_id"`
	GuildIDs       []string `json:"guild_ids"`
	ChannelIDs     []string `json:"channel_ids"`
	UserIDs        []string `json:"user_ids"`
	ProjectID      string   `json:"project_id"`
	ProviderID     string   `json:"provider_id"`
	ContextProfile string   `json:"context_profile"`
}
type Status struct {
	Config               Config `json:"config"`
	TokenStored          bool   `json:"token_stored"`
	State                string `json:"state"`
	LastError            string `json:"last_error"`
	BotID                string `json:"bot_id,omitempty"`
	ActiveRequests       int    `json:"active_requests"`
	CredentialProtection string `json:"credential_protection"`
}
type Service struct {
	store              *store.Store
	vault              *secrets.Vault
	backend            Backend
	lifetime           context.Context
	opMu               sync.Mutex
	mu                 sync.Mutex
	rateMu             sync.Mutex
	rateUntil          time.Time
	lastPrune          time.Time
	state              string
	lastError          string
	botID              string
	cancel             context.CancelFunc
	jobs               map[string]context.CancelFunc
	gatewayWG          sync.WaitGroup
	jobsWG             sync.WaitGroup
	client             *http.Client
	dialer             *websocket.Dialer
	apiBase            string
	allowTestEndpoints bool
	reconnectDelay     time.Duration
}

func NewService(ctx context.Context, dataStore *store.Store, vault *secrets.Vault, backend Backend) (*Service, error) {
	if dataStore == nil || vault == nil || backend == nil {
		return nil, errors.New("Discord bridge requires store, vault and agent backend")
	}
	_, err := dataStore.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS discord_bridge_config (
 id INTEGER PRIMARY KEY CHECK(id=1), owner_principal_id TEXT NOT NULL, config_json TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS discord_bridge_bindings (
 binding_key TEXT PRIMARY KEY, scope_hash TEXT NOT NULL, session_id TEXT NOT NULL, updated_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS discord_bridge_interactions (
 interaction_id TEXT PRIMARY KEY, command TEXT NOT NULL, binding_key TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS discord_bridge_approvals (
 binding_key TEXT NOT NULL, approval_id TEXT NOT NULL, session_id TEXT NOT NULL, arguments_hash TEXT NOT NULL,
 expires_at TEXT NOT NULL, PRIMARY KEY(binding_key,approval_id));`)
	if err != nil {
		return nil, err
	}
	return &Service{store: dataStore, vault: vault, backend: backend, lifetime: ctx, state: "disabled", jobs: map[string]context.CancelFunc{}, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, dialer: &websocket.Dialer{HandshakeTimeout: 10 * time.Second}, apiBase: "https://discord.com/api/v10", reconnectDelay: time.Second}, nil
}

func (s *Service) loadConfig(ctx context.Context) (Config, string, error) {
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = errors.Join(ErrForbidden, err)
		}
		return Config{}, "", err
	}
	var raw, savedOwner string
	err = s.store.DB.QueryRowContext(ctx, `SELECT config_json,owner_principal_id FROM discord_bridge_config WHERE id=1`).Scan(&raw, &savedOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{GuildIDs: []string{}, ChannelIDs: []string{}, UserIDs: []string{}, ContextProfile: "compact-32k"}, owner, nil
	}
	if err != nil {
		return Config{}, "", err
	}
	if savedOwner != owner {
		return Config{}, "", errors.Join(ErrForbidden, errors.New("Discord configuration belongs to another principal"))
	}
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return Config{}, "", errors.New("Discord configuration is invalid")
	}
	return cfg, owner, nil
}
func (s *Service) saveConfig(ctx context.Context, cfg Config, owner string) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO discord_bridge_config(id,owner_principal_id,config_json) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET owner_principal_id=excluded.owner_principal_id,config_json=excluded.config_json`, owner, string(raw))
	return err
}
func (s *Service) saveAndResetBindings(ctx context.Context, cfg Config, owner string) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO discord_bridge_config(id,owner_principal_id,config_json) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET owner_principal_id=excluded.owner_principal_id,config_json=excluded.config_json`, owner, string(raw)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM discord_bridge_bindings; DELETE FROM discord_bridge_approvals;`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) Status(ctx context.Context) (Status, error) {
	cfg, _, err := s.loadConfig(ctx)
	if err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state
	if !cfg.Enabled && s.cancel == nil && state != "error" {
		state = "disabled"
	} else if cfg.Enabled && s.cancel == nil && state == "disabled" {
		state = "stopped"
	}
	return Status{Config: cfg, TokenStored: s.vault.Has(tokenRef), State: state, LastError: s.lastError, BotID: s.botID, ActiveRequests: len(s.jobs), CredentialProtection: secrets.Protection()}, nil
}
func (s *Service) Configure(ctx context.Context, cfg Config) (Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	_, owner, err := s.loadConfig(ctx)
	if err != nil {
		return Status{}, err
	}
	cfg.Enabled = false
	cfg.ApplicationID = strings.TrimSpace(cfg.ApplicationID)
	cfg.ProjectID = strings.TrimSpace(cfg.ProjectID)
	cfg.ProviderID = strings.TrimSpace(cfg.ProviderID)
	if cfg.ContextProfile == "" {
		cfg.ContextProfile = "compact-32k"
	}
	if err := validateConfig(cfg, false); err != nil {
		return Status{}, err
	}
	s.stopRuntime()
	if err := s.saveAndResetBindings(ctx, cfg, owner); err != nil {
		return Status{}, err
	}
	s.setState("disabled", "")
	return s.Status(ctx)
}
func validateConfig(cfg Config, complete bool) (err error) {
	defer func() {
		if err != nil {
			err = errors.Join(ErrInvalidConfig, err)
		}
	}()
	if (complete || cfg.ApplicationID != "") && !snowflakePattern.MatchString(cfg.ApplicationID) {
		return errors.New("application_id must be a Discord numeric ID (17–20 digits)")
	}
	for name, ids := range map[string][]string{"guild_ids": cfg.GuildIDs, "channel_ids": cfg.ChannelIDs, "user_ids": cfg.UserIDs} {
		if len(ids) > 50 || (complete && len(ids) == 0) {
			return fmt.Errorf("%s must contain 1–50 explicit Discord IDs", name)
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if !snowflakePattern.MatchString(id) || seen[id] {
				return fmt.Errorf("%s must contain unique numeric Discord IDs; wildcards are not allowed", name)
			}
			seen[id] = true
		}
	}
	if len(cfg.ProjectID) > 128 || len(cfg.ProviderID) > 128 || len(cfg.ContextProfile) > 64 {
		return errors.New("project/provider/context values exceed limits")
	}
	if complete && (cfg.ProjectID == "" || cfg.ProviderID == "" || cfg.ContextProfile == "") {
		return errors.New("choose one project, local provider and context profile")
	}
	return nil
}
func (s *Service) SetToken(ctx context.Context, token string) (Status, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	cfg, owner, err := s.loadConfig(ctx)
	if err != nil {
		return Status{}, err
	}
	token = strings.TrimSpace(token)
	if len(token) > 4096 || strings.ContainsAny(token, "\r\n\x00") {
		return Status{}, errors.Join(ErrInvalidConfig, errors.New("invalid Discord bot token"))
	}
	s.stopRuntime()
	cfg.Enabled = false
	if err := s.saveAndResetBindings(ctx, cfg, owner); err != nil {
		return Status{}, err
	}
	if err := s.vault.Set(tokenRef, token); err != nil {
		return Status{}, err
	}
	s.setState("disabled", "")
	return s.Status(ctx)
}
func (s *Service) Start(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	cfg, owner, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	if err := validateConfig(cfg, true); err != nil {
		return err
	}
	if !s.vault.Has(tokenRef) {
		return errors.Join(ErrInvalidConfig, errors.New("save the Discord bot token before connecting"))
	}
	if err := s.backend.Validate(ctx, binding(cfg, owner)); err != nil {
		return errors.Join(ErrInvalidConfig, err)
	}
	if s.lifetime.Err() != nil {
		return errors.New("application is stopping")
	}
	s.stopRuntime()
	cfg.Enabled = true
	if err := s.saveConfig(ctx, cfg, owner); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(s.lifetime)
	s.mu.Lock()
	s.cancel = cancel
	s.state = "connecting"
	s.lastError = ""
	s.botID = ""
	s.mu.Unlock()
	s.gatewayWG.Add(1)
	go func() { defer s.gatewayWG.Done(); s.runGateway(runCtx, cfg, owner) }()
	return nil
}
func (s *Service) Stop(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	cfg, owner, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	s.stopRuntime()
	cfg.Enabled = false
	if err := s.saveConfig(ctx, cfg, owner); err != nil {
		return err
	}
	s.setState("disabled", "")
	return nil
}
func (s *Service) Close() { s.opMu.Lock(); defer s.opMu.Unlock(); s.stopRuntime() }
func (s *Service) stopRuntime() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	if cancel != nil {
		cancel()
	}
	for _, cancelJob := range s.jobs {
		cancelJob()
	}
	s.mu.Unlock()
	s.gatewayWG.Wait()
	s.jobsWG.Wait()
	s.mu.Lock()
	s.state = "stopped"
	s.mu.Unlock()
}
func (s *Service) setState(state, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	s.lastError = message
}
func binding(cfg Config, owner string) Binding {
	return Binding{OwnerPrincipalID: owner, ProjectID: cfg.ProjectID, ProviderID: cfg.ProviderID, ContextProfile: cfg.ContextProfile}
}
func scopeHash(cfg Config, owner string) string {
	raw, _ := json.Marshal(binding(cfg, owner))
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func bindingKey(guild, channel, user string) string { return guild + ":" + channel + ":" + user }
