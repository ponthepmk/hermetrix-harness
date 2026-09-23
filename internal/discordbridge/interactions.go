package discordbridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type interactionOption struct {
	Name    string              `json:"name"`
	Type    int                 `json:"type"`
	Value   json.RawMessage     `json:"value"`
	Options []interactionOption `json:"options"`
}
type discordUser struct {
	ID  string `json:"id"`
	Bot bool   `json:"bot"`
}
type interaction struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	Type          int    `json:"type"`
	Token         string `json:"token"`
	GuildID       string `json:"guild_id"`
	ChannelID     string `json:"channel_id"`
	Member        *struct {
		User discordUser `json:"user"`
	} `json:"member"`
	User *discordUser `json:"user"`
	Data struct {
		Name    string              `json:"name"`
		Type    int                 `json:"type"`
		Options []interactionOption `json:"options"`
	} `json:"data"`
}

func containsID(items []string, id string) bool {
	for _, item := range items {
		if item == id {
			return true
		}
	}
	return false
}
func (event interaction) allowed(cfg Config) bool {
	return event.Type == 2 && event.ApplicationID == cfg.ApplicationID && event.GuildID != "" && containsID(cfg.GuildIDs, event.GuildID) && containsID(cfg.ChannelIDs, event.ChannelID) && event.Member != nil && !event.Member.User.Bot && (event.User == nil || !event.User.Bot) && containsID(cfg.UserIDs, event.Member.User.ID)
}
func freshInteraction(id string, now time.Time) bool {
	if !snowflakePattern.MatchString(id) {
		return false
	}
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return false
	}
	created := time.UnixMilli(int64(value>>22) + 1420070400000)
	return created.After(now.Add(-15*time.Minute)) && created.Before(now.Add(time.Minute))
}
func parseCommand(event interaction) (string, string, error) {
	if event.Data.Name != "hermetrix" || (event.Data.Type != 0 && event.Data.Type != 1) || len(event.Data.Options) != 1 {
		return "", "", errors.New("use a /hermetrix subcommand")
	}
	option := event.Data.Options[0]
	if option.Type != 1 {
		return "", "", errors.New("invalid command")
	}
	arg := ""
	switch option.Name {
	case "new", "status", "cancel":
		if len(option.Options) != 0 {
			return "", "", errors.New("unexpected command option")
		}
	case "ask", "approve", "deny":
		if len(option.Options) != 1 || option.Options[0].Type != 3 {
			return "", "", errors.New("a text command option is required")
		}
		expected := "approval_id"
		maximum := 200
		if option.Name == "ask" {
			expected = "prompt"
			maximum = 4000
		}
		if option.Options[0].Name != expected || json.Unmarshal(option.Options[0].Value, &arg) != nil || strings.TrimSpace(arg) == "" || textUnits(arg) > maximum {
			return "", "", errors.New("invalid or oversized command option")
		}
	default:
		return "", "", errors.New("unknown /hermetrix command")
	}
	return option.Name, arg, nil
}

func (s *Service) handleInteraction(ctx context.Context, cfg Config, owner string, event interaction) {
	if ctx.Err() != nil || event.ApplicationID != cfg.ApplicationID || !freshInteraction(event.ID, time.Now()) || len(event.Token) < 1 || len(event.Token) > 2048 {
		return
	}
	userID := ""
	if event.Member != nil {
		userID = event.Member.User.ID
	}
	key := bindingKey(event.GuildID, event.ChannelID, userID)
	command, arg, parseErr := parseCommand(event)
	ackCtx, ackCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer ackCancel()
	claimCtx, claimCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer claimCancel()
	s.pruneInteractionHistory(claimCtx)
	result, err := s.store.DB.ExecContext(claimCtx, `INSERT OR IGNORE INTO discord_bridge_interactions(interaction_id,command,binding_key,state,created_at) VALUES(?,?,?,'claimed',?)`, event.ID, command, key, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return
	}
	if !event.allowed(cfg) {
		_ = s.acknowledge(ackCtx, event, "This Discord user, server or channel is not allowed to control Hermetrix.", false)
		s.finishInteraction(event.ID, "denied")
		return
	}
	if parseErr != nil {
		_ = s.acknowledge(ackCtx, event, parseErr.Error(), false)
		s.finishInteraction(event.ID, "rejected")
		return
	}
	if command == "cancel" {
		if err := s.acknowledge(ackCtx, event, "Cancellation requested. An already approved file/tool effect may finish; inspect its receipt locally.", false); err != nil {
			s.finishInteraction(event.ID, "reply_failed")
			return
		}
		s.mu.Lock()
		cancel := s.jobs[key]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		s.finishInteraction(event.ID, "completed")
		return
	}
	slotKey := key
	if command == "status" {
		slotKey = key + ":status"
	}
	s.mu.Lock()
	_, busy := s.jobs[slotKey]
	full := len(s.jobs) >= 4
	if busy || full {
		s.mu.Unlock()
		_ = s.acknowledge(ackCtx, event, "A request is already running or the bridge is busy. Use /hermetrix cancel or try again after it finishes.", false)
		s.finishInteraction(event.ID, "busy")
		return
	}
	jobCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	s.jobs[slotKey] = cancel
	s.jobsWG.Add(1)
	s.mu.Unlock()
	if err := s.acknowledge(ackCtx, event, "", true); err != nil {
		cancel()
		s.mu.Lock()
		delete(s.jobs, slotKey)
		s.mu.Unlock()
		s.jobsWG.Done()
		s.finishInteraction(event.ID, "reply_failed")
		return
	}
	go func() {
		defer s.jobsWG.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.jobs, slotKey); s.mu.Unlock() }()
		reply, sessionID, err := s.executeCommand(jobCtx, cfg, owner, key, userID, command, arg)
		content := reply.Text
		state := "completed"
		if err != nil {
			content = "Hermetrix: " + err.Error()
			state = "failed"
		}
		approval := reply.Approval
		remoteApproval := false
		if err == nil && approval != nil {
			content, remoteApproval = formatApproval(*approval)
		}
		if deliveryErr := s.editResponse(jobCtx, event, content); deliveryErr != nil {
			s.finishInteraction(event.ID, "reply_failed")
			s.mu.Lock()
			s.lastError = "A Discord reply could not be delivered. Effects are not retried; inspect the local session."
			s.mu.Unlock()
			return
		}
		if remoteApproval && approval != nil {
			_, saveErr := s.store.DB.ExecContext(jobCtx, `INSERT INTO discord_bridge_approvals(binding_key,approval_id,session_id,arguments_hash,expires_at) VALUES(?,?,?,?,?) ON CONFLICT(binding_key,approval_id) DO UPDATE SET session_id=excluded.session_id,arguments_hash=excluded.arguments_hash,expires_at=excluded.expires_at`, key, approval.ID, sessionID, approval.ArgumentsHash, time.Now().Add(30*time.Minute).UTC().Format(time.RFC3339Nano))
			if saveErr != nil {
				state = "approval_binding_failed"
			}
		}
		s.finishInteraction(event.ID, state)
	}()
}

func formatApproval(approval ApprovalSummary) (string, bool) {
	if approval.ID == "" || approval.ArgumentsHash == "" || textUnits(approval.Preview) > 1200 || textUnits(approval.Summary) > 200 || strings.Contains(approval.Preview, "```") {
		return "Approval required: " + truncateMessage(approval.Summary, 500) + "\nThe full preview must be reviewed in the local Hermetrix app. Remote approval is unavailable for this change.", false
	}
	preview := approval.Preview
	text := "Approval required: " + approval.Summary + "\nTool: " + approval.ToolName + "\nExact proposed content:\n```\n" + preview + "\n```\nApprove: /hermetrix approve approval_id:" + approval.ID + "\nDeny: /hermetrix deny approval_id:" + approval.ID
	if textUnits(text) > 1900 {
		return "A change needs approval. Open Hermetrix locally to review its complete preview.", false
	}
	return text, true
}
func (s *Service) finishInteraction(id, state string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = s.store.DB.ExecContext(ctx, `UPDATE discord_bridge_interactions SET state=? WHERE interaction_id=?`, state, id)
}

func (s *Service) pruneInteractionHistory(ctx context.Context) {
	s.mu.Lock()
	if time.Since(s.lastPrune) < time.Hour {
		s.mu.Unlock()
		return
	}
	s.lastPrune = time.Now()
	s.mu.Unlock()
	// IDs older than 15 minutes cannot execute at ingress. Keep 24 hours of
	// replay receipts, deleting only a bounded batch on each maintenance pass.
	_, _ = s.store.DB.ExecContext(ctx, `DELETE FROM discord_bridge_interactions WHERE interaction_id IN (SELECT interaction_id FROM discord_bridge_interactions WHERE created_at<? LIMIT 512)`, time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339Nano))
	_, _ = s.store.DB.ExecContext(ctx, `DELETE FROM discord_bridge_approvals WHERE rowid IN (SELECT rowid FROM discord_bridge_approvals WHERE expires_at<? LIMIT 512)`, time.Now().UTC().Format(time.RFC3339Nano))
}
func (s *Service) sessionBinding(ctx context.Context, key, scope string) (string, error) {
	var id, stored string
	err := s.store.DB.QueryRowContext(ctx, `SELECT session_id,scope_hash FROM discord_bridge_bindings WHERE binding_key=?`, key).Scan(&id, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if stored != scope {
		return "", nil
	}
	return id, nil
}
func (s *Service) bindSession(ctx context.Context, key, scope, id string) error {
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO discord_bridge_bindings(binding_key,scope_hash,session_id,updated_at) VALUES(?,?,?,?) ON CONFLICT(binding_key) DO UPDATE SET scope_hash=excluded.scope_hash,session_id=excluded.session_id,updated_at=excluded.updated_at`, key, scope, id, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Service) executeCommand(ctx context.Context, cfg Config, owner, key, userID, command, arg string) (Reply, string, error) {
	scope := binding(cfg, owner)
	if err := s.backend.Validate(ctx, scope); err != nil {
		return Reply{}, "", err
	}
	sessionID, err := s.sessionBinding(ctx, key, scopeHash(cfg, owner))
	if err != nil {
		return Reply{}, "", err
	}
	if command == "new" || command == "ask" && sessionID == "" {
		if sessionID != "" {
			old, err := s.backend.Status(ctx, scope, sessionID)
			if err != nil {
				return Reply{}, sessionID, err
			}
			if old.Approval != nil {
				return Reply{}, sessionID, errors.New("resolve the pending approval before starting a new session")
			}
		}
		id, err := s.backend.New(ctx, scope, "Discord project session")
		if err != nil {
			return Reply{}, "", err
		}
		if err := s.bindSession(ctx, key, scopeHash(cfg, owner), id); err != nil {
			return Reply{}, id, err
		}
		sessionID = id
		if command == "new" {
			return Reply{Text: "New Hermetrix session ready. Use /hermetrix ask prompt:…"}, sessionID, nil
		}
	}
	if sessionID == "" {
		return Reply{Text: "No session is linked for you in this channel. Use /hermetrix new or /hermetrix ask."}, "", nil
	}
	switch command {
	case "ask":
		reply, err := s.backend.Ask(ctx, scope, sessionID, arg)
		return reply, sessionID, err
	case "status":
		reply, err := s.backend.Status(ctx, scope, sessionID)
		return reply, sessionID, err
	case "approve", "deny":
		var storedSession, hash, expires string
		err := s.store.DB.QueryRowContext(ctx, `SELECT session_id,arguments_hash,expires_at FROM discord_bridge_approvals WHERE binding_key=? AND approval_id=?`, key, arg).Scan(&storedSession, &hash, &expires)
		if err != nil || storedSession != sessionID {
			return Reply{}, sessionID, errors.New("approval was not fully shown to this user in this channel; use /hermetrix status or the local app")
		}
		deadline, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil || time.Now().After(deadline) {
			return Reply{}, sessionID, errors.New("approval preview expired; use /hermetrix status to review it again")
		}
		pending, err := s.backend.GetApproval(ctx, scope, sessionID, arg)
		if err != nil {
			return Reply{}, sessionID, err
		}
		if pending.SessionID != sessionID || pending.ArgumentsHash != hash {
			return Reply{}, sessionID, errors.New("approval changed; review it again locally")
		}
		_, err = s.store.DB.ExecContext(ctx, `DELETE FROM discord_bridge_approvals WHERE binding_key=? AND approval_id=?`, key, arg)
		if err != nil {
			return Reply{}, sessionID, err
		}
		reply, err := s.backend.Decide(ctx, scope, sessionID, arg, command, "discord:"+userID)
		return reply, sessionID, err
	default:
		return Reply{}, sessionID, fmt.Errorf("unsupported command %s", command)
	}
}
