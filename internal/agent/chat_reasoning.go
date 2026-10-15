package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/providers"
)

func (s *Service) validateChatReasoning(ctx context.Context, provider providers.Profile, reserve int, setting ChatReasoningSetting) error {
	if setting.Mode != "disabled" && setting.Mode != "bounded" {
		return fmt.Errorf("chat reasoning mode must be disabled or bounded")
	}
	limit := min(reserve, provider.MaxOutputTokens)
	if setting.TokenCap < 0 || setting.TokenCap > limit-512 || (setting.Mode == "disabled" && setting.TokenCap != 0) {
		return fmt.Errorf("chat reasoning must leave at least 512 output tokens for answers/tools")
	}
	if !s.providers.SupportsChatReasoningBudget(ctx, provider) {
		return fmt.Errorf("selected runtime has no verified bounded-reasoning transport")
	}
	return nil
}

// Explicit idle-session reconfiguration. Existing frozen tool authority and
// provider qualification are retained; no approval, stop or quarantine is reset.
func (s *Service) SetSessionReasoning(ctx context.Context, id string, input SessionReasoningInput) (Session, error) {
	reason := strings.TrimSpace(input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > 1000 {
		return Session{}, fmt.Errorf("reasoning change requires a reason of 1 to 1000 characters")
	}
	session, err := s.GetSession(ctx, id)
	if err != nil {
		return Session{}, err
	}
	if session.State != "active" || session.ActiveTurnID != "" {
		return Session{}, fmt.Errorf("finish or stop the current turn before changing reasoning")
	}
	provider, err := s.providers.Get(ctx, session.ProviderID)
	if err != nil {
		return Session{}, err
	}
	profile, ok := ctxcompiler.ProfileByName(session.ContextProfile)
	if !ok {
		return Session{}, fmt.Errorf("unknown session context profile")
	}
	setting := ChatReasoningSetting{Mode: input.Mode, TokenCap: input.TokenCap}
	if err = s.validateChatReasoning(ctx, provider, profile.OutputReserve, setting); err != nil {
		return Session{}, err
	}
	oldRevision := session.ContractRevision
	session.Contract.ChatReasoning = &setting
	session.Contract.AnswerBudget = min(profile.OutputReserve, provider.MaxOutputTokens) - setting.TokenCap
	session.CacheEpoch++
	session.Contract.CacheEpoch = session.CacheEpoch
	session.Contract.Revision = sessionContractRevision(session.Contract)
	session.ContractRevision = session.Contract.Revision
	session.UpdatedAt = time.Now().UTC()
	encoded, err := json.Marshal(session.Contract)
	if err != nil {
		return Session{}, err
	}
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE agent_sessions SET contract_json=?,contract_revision=?,cache_epoch=?,updated_at=? WHERE id=? AND owner_principal_id=? AND contract_revision=? AND state='active' AND COALESCE(active_turn_id,'')=''`,
		string(encoded), session.ContractRevision, session.CacheEpoch, formatTime(session.UpdatedAt), id, session.OwnerPrincipalID, oldRevision)
	if err != nil {
		return Session{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Session{}, fmt.Errorf("session changed during reasoning update")
	}
	sequence, err := nextSequence(ctx, tx, id)
	if err != nil {
		return Session{}, err
	}
	metadata, _ := json.Marshal(map[string]any{"actor": session.OwnerPrincipalID, "reason": reason, "old_contract_revision": oldRevision, "session_contract_revision": session.ContractRevision, "mode": setting.Mode, "token_cap": setting.TokenCap})
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_events(id,session_id,turn_id,sequence,event_kind,role,content,metadata_json,created_at) VALUES(?,?,'',?,'session_reasoning_changed','user',?,?,?)`,
		identity.New("event"), id, sequence, "ปรับงบการคิดของแชตเพื่อเหลือพื้นที่สำหรับคำตอบและการใช้เครื่องมือ", string(metadata), formatTime(session.UpdatedAt))
	if err != nil {
		return Session{}, err
	}
	if err = tx.Commit(); err != nil {
		return Session{}, err
	}
	return session, nil
}
