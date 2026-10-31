//go:build windows

package product

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hermetrix-harness/internal/durability"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

func (s *Service) PrepareDesktopAction(ctx context.Context, input DesktopActionInput) (DesktopAction, error) {
	if input.Action != "invoke" && input.Action != "set_value" {
		return DesktopAction{}, fmt.Errorf("desktop action must be invoke or set_value")
	}
	if (input.ElementRef < 1 && input.Point == nil) || input.Handle == 0 || input.ProcessID < 1 || input.ObservationHash == "" ||
		(input.Action == "set_value" && (len(input.Text) > 4096 || strings.ContainsAny(input.Text, "\r\n"))) {
		return DesktopAction{}, fmt.Errorf("desktop action requires an exact window, element and current observation")
	}
	if input.Point != nil && (input.Action != "invoke" || input.ElementRef != 0 || input.Text != "") {
		return DesktopAction{}, fmt.Errorf("pointer invocation requires only one point and no element or text")
	}
	observation, err := s.ObserveDesktopWindow(ctx, input.Handle, input.ProcessID)
	if err != nil {
		return DesktopAction{}, err
	}
	if observation.ObservationHash != input.ObservationHash {
		return DesktopAction{}, fmt.Errorf("desktop observation changed; inspect the window again")
	}
	if observation.ScreenshotArtifactID == "" {
		return DesktopAction{}, fmt.Errorf("window image is unavailable; inspect a visible window before acting")
	}
	if input.ElementRef > len(observation.Elements) {
		return DesktopAction{}, fmt.Errorf("desktop element reference is stale")
	}
	var target DesktopElement
	if input.Point != nil {
		b := observation.Window.Bounds
		if input.Point.X < 0 || input.Point.Y < 0 || input.Point.X >= b.Width || input.Point.Y >= b.Height {
			return DesktopAction{}, fmt.Errorf("point is outside the window image")
		}
		target = DesktopElement{Name: fmt.Sprintf("Click (%d, %d)", input.Point.X, input.Point.Y), ControlType: "CoordinateClick", Enabled: true, Bounds: b, Point: input.Point}
	} else {
		target = observation.Elements[input.ElementRef-1]
	}
	if !target.Enabled {
		return DesktopAction{}, fmt.Errorf("desktop element is disabled")
	}
	if input.Action == "set_value" && target.IsPassword {
		return DesktopAction{}, fmt.Errorf("password fields are unavailable to desktop set_value")
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return DesktopAction{}, err
	}
	if input.TaskID != "" {
		taskOwner, taskErr := s.researchTaskOwner(ctx, input.TaskID)
		if taskErr != nil || taskOwner != ownerID {
			return DesktopAction{}, fmt.Errorf("desktop action Task is unavailable to this principal")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	durability.Exec("expire prepared desktop actions").Observe(s.store.DB.ExecContext(ctx, `UPDATE desktop_actions SET state='cancelled',completed_at=? WHERE owner_principal_id=? AND state='prepared' AND expires_at<=?`,
		formatTime(time.Now().UTC()), ownerID, formatTime(time.Now().UTC())))
	var unresolved int
	if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM desktop_actions da WHERE da.owner_principal_id=? AND da.window_handle=? AND da.process_id=?
		AND (da.state IN ('prepared','dispatched') OR (da.state='uncertain' AND NOT EXISTS
		(SELECT 1 FROM desktop_action_reconciliations dr WHERE dr.action_id=da.id)))`, ownerID, input.Handle, input.ProcessID).Scan(&unresolved); err != nil {
		return DesktopAction{}, err
	}
	if unresolved > 0 {
		return DesktopAction{}, fmt.Errorf("a Windows action for this window is unresolved; cancel or reconcile it before another action")
	}
	textSum := sha256.Sum256([]byte(input.Text))
	textHash := hex.EncodeToString(textSum[:])
	targetJSON, _ := json.Marshal(target)
	subject, _ := json.Marshal(struct {
		Owner           string
		TaskID          string
		Handle          int64
		ProcessID       int
		Action          string
		Ref             int
		Target          DesktopElement
		TextHash        string
		ObservationHash string
	}{ownerID, input.TaskID, input.Handle, input.ProcessID, input.Action, input.ElementRef, target, textHash, input.ObservationHash})
	subjectSum := sha256.Sum256(subject)
	now := time.Now().UTC()
	action := DesktopAction{ID: identity.New("desktopaction"), TaskID: input.TaskID, Handle: input.Handle, ProcessID: input.ProcessID,
		Action: input.Action, ElementRef: input.ElementRef, Target: target, ObservationHash: input.ObservationHash,
		SubjectHash: hex.EncodeToString(subjectSum[:]), State: "prepared", CreatedAt: now, ExpiresAt: now.Add(2 * time.Minute), ScreenshotBeforeID: observation.ScreenshotArtifactID}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO desktop_actions(id,owner_principal_id,task_id,window_handle,process_id,action,element_ref,
		target_json,text_hash,observation_hash,subject_hash,state,created_at,expires_at,screenshot_before_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,'prepared',?,?,?)`,
		action.ID, ownerID, input.TaskID, input.Handle, input.ProcessID, input.Action, input.ElementRef, string(targetJSON), textHash,
		input.ObservationHash, action.SubjectHash, formatTime(now), formatTime(action.ExpiresAt), action.ScreenshotBeforeID)
	return action, err
}

func (s *Service) GetDesktopAction(ctx context.Context, id string) (DesktopAction, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return DesktopAction{}, err
	}
	var item DesktopAction
	var targetJSON, created, expires, observationJSON string
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,window_handle,process_id,action,element_ref,target_json,
		observation_hash,subject_hash,state,error,created_at,expires_at,screenshot_before_id,screenshot_after_id,observation_json FROM desktop_actions WHERE id=? AND owner_principal_id=?`, id, ownerID).
		Scan(&item.ID, &item.TaskID, &item.Handle, &item.ProcessID, &item.Action, &item.ElementRef, &targetJSON,
			&item.ObservationHash, &item.SubjectHash, &item.State, &item.Error, &created, &expires, &item.ScreenshotBeforeID, &item.ScreenshotAfterID, &observationJSON)
	if err != nil {
		return DesktopAction{}, err
	}
	if err := json.Unmarshal([]byte(targetJSON), &item.Target); err != nil {
		return DesktopAction{}, err
	}
	if observationJSON != "" {
		if err := json.Unmarshal([]byte(observationJSON), &item.Observation); err != nil {
			return DesktopAction{}, err
		}
	}
	item.CreatedAt, _ = parseTime(created)
	item.ExpiresAt, _ = parseTime(expires)
	var reconciliation DesktopActionReconciliation
	var reconciledAt string
	err = s.store.DB.QueryRowContext(ctx, `SELECT action_id,resolution,observation_hash,screenshot_artifact_id,actor,note,created_at
		FROM desktop_action_reconciliations WHERE action_id=? AND owner_principal_id=?`, id, ownerID).
		Scan(&reconciliation.ActionID, &reconciliation.Resolution, &reconciliation.ObservationHash,
			&reconciliation.ScreenshotArtifactID, &reconciliation.Actor, &reconciliation.Note, &reconciledAt)
	if err != nil && err != sql.ErrNoRows {
		return DesktopAction{}, err
	}
	if err == nil {
		reconciliation.CreatedAt, _ = parseTime(reconciledAt)
		item.Reconciliation = &reconciliation
	}
	return item, nil
}

func (s *Service) ReconcileDesktopAction(ctx context.Context, id, resolution, observationHash, actor, note string) (DesktopAction, error) {
	if resolution != "observed_applied" && resolution != "observed_not_applied" {
		return DesktopAction{}, fmt.Errorf("desktop reconciliation requires an explicit observed outcome")
	}
	actor, note = strings.TrimSpace(actor), strings.TrimSpace(note)
	if actor == "" || len(note) < 8 || len(note) > 2000 || observationHash == "" {
		return DesktopAction{}, fmt.Errorf("desktop reconciliation requires an actor, observation and description")
	}
	item, err := s.GetDesktopAction(ctx, id)
	if err != nil {
		return DesktopAction{}, err
	}
	if item.State != "uncertain" || item.Reconciliation != nil {
		return DesktopAction{}, fmt.Errorf("desktop action is not awaiting reconciliation")
	}
	observation, err := s.ObserveDesktopWindow(ctx, item.Handle, item.ProcessID)
	if err != nil {
		return DesktopAction{}, err
	}
	if observation.ObservationHash != observationHash || observation.ScreenshotArtifactID == "" {
		return DesktopAction{}, fmt.Errorf("desktop observation changed or has no screenshot; inspect again")
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return DesktopAction{}, err
	}
	result, err := s.store.DB.ExecContext(ctx, `INSERT INTO desktop_action_reconciliations(action_id,owner_principal_id,resolution,
		observation_hash,screenshot_artifact_id,actor,note,created_at)
		SELECT id,owner_principal_id,?,?,?,?,?,? FROM desktop_actions
		WHERE id=? AND owner_principal_id=? AND state='uncertain'`, resolution, observation.ObservationHash,
		observation.ScreenshotArtifactID, actor, note, formatTime(time.Now().UTC()), id, ownerID)
	if err != nil {
		return DesktopAction{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return DesktopAction{}, fmt.Errorf("desktop action is no longer awaiting reconciliation")
	}
	return s.GetDesktopAction(ctx, id)
}

func (s *Service) ListDesktopActions(ctx context.Context, taskID string) ([]DesktopAction, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id FROM desktop_actions WHERE task_id=? AND owner_principal_id=? ORDER BY created_at DESC LIMIT 100`, taskID, ownerID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := []DesktopAction{}
	for _, id := range ids {
		item, err := s.GetDesktopAction(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) CancelDesktopAction(ctx context.Context, id, subjectHash string) (DesktopAction, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return DesktopAction{}, err
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE desktop_actions SET state='cancelled',completed_at=? WHERE id=? AND owner_principal_id=?
		AND subject_hash=? AND state='prepared'`, formatTime(time.Now().UTC()), id, ownerID, subjectHash)
	if err != nil {
		return DesktopAction{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return DesktopAction{}, fmt.Errorf("desktop action is stale or already dispatched")
	}
	return s.GetDesktopAction(ctx, id)
}

func (s *Service) ConfirmDesktopAction(ctx context.Context, id, subjectHash, textValue, actor string) (DesktopAction, error) {
	item, err := s.GetDesktopAction(ctx, id)
	if err != nil {
		return DesktopAction{}, err
	}
	if item.State != "prepared" || item.SubjectHash != subjectHash || time.Now().UTC().After(item.ExpiresAt) || strings.TrimSpace(actor) == "" {
		return DesktopAction{}, fmt.Errorf("desktop approval is stale, expired or already used")
	}
	if item.Action == "invoke" && textValue != "" {
		return DesktopAction{}, fmt.Errorf("invoke action cannot carry text")
	}
	if item.Action == "set_value" && (len(textValue) > 4096 || strings.ContainsAny(textValue, "\r\n")) {
		return DesktopAction{}, fmt.Errorf("desktop text is invalid or too long")
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return DesktopAction{}, err
	}
	var storedTextHash string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT text_hash FROM desktop_actions WHERE id=? AND owner_principal_id=?`, id, ownerID).Scan(&storedTextHash); err != nil {
		return DesktopAction{}, err
	}
	textSum := sha256.Sum256([]byte(textValue))
	if hex.EncodeToString(textSum[:]) != storedTextHash {
		return DesktopAction{}, fmt.Errorf("desktop text changed after review")
	}
	// Pointer input binds the complete reviewed frame. Element input also rechecks
	// its exact UIA target in the native probe immediately before dispatch.
	current, err := s.ObserveDesktopWindow(ctx, item.Handle, item.ProcessID)
	if err != nil {
		return DesktopAction{}, err
	}
	if (item.Target.Point != nil && current.ObservationHash != item.ObservationHash) || current.ScreenshotArtifactID == "" {
		return DesktopAction{}, fmt.Errorf("desktop observation changed after approval; inspect again")
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE desktop_actions SET state='dispatched',actor=?,dispatched_at=? WHERE id=? AND owner_principal_id=?
		AND subject_hash=? AND state='prepared' AND expires_at>?`, strings.TrimSpace(actor), formatTime(time.Now().UTC()), id, ownerID, subjectHash, formatTime(time.Now().UTC()))
	if err != nil {
		return DesktopAction{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return DesktopAction{}, fmt.Errorf("desktop approval was used or expired")
	}
	var actionResult map[string]any
	if item.Target.Point != nil {
		err = clickDesktopPoint(ctx, item)
	} else {
		err = runDesktopProbe(ctx, map[string]any{"mode": "action", "handle": item.Handle, "process_id": item.ProcessID,
			"action": item.Action, "element_ref": item.ElementRef, "target": item.Target, "text": textValue}, &actionResult)
	}
	if err != nil {
		state := "uncertain"
		if errors.Is(err, errDesktopNotDispatched) {
			state = "cancelled"
		}
		durability.Exec("record desktop action failure").Observe(s.store.DB.ExecContext(context.Background(), `UPDATE desktop_actions SET state=?,error=?,completed_at=? WHERE id=? AND state='dispatched'`,
			state, err.Error(), formatTime(time.Now().UTC()), id))
		return s.GetDesktopAction(context.Background(), id)
	}
	observation, observeErr := s.ObserveDesktopWindow(ctx, item.Handle, item.ProcessID)
	if observeErr != nil || observation.ScreenshotArtifactID == "" {
		if observeErr == nil {
			observeErr = fmt.Errorf("follow-up screenshot unavailable: %s", observation.ScreenshotError)
		}
		durability.Exec("record uncertain desktop observation").Observe(s.store.DB.ExecContext(context.Background(), `UPDATE desktop_actions SET state='uncertain',error=?,completed_at=? WHERE id=? AND state='dispatched'`,
			"action returned but follow-up observation failed: "+observeErr.Error(), formatTime(time.Now().UTC()), id))
		return s.GetDesktopAction(context.Background(), id)
	}
	observationBody, _ := json.Marshal(observation)
	_, err = s.store.DB.ExecContext(context.Background(), `UPDATE desktop_actions SET state='completed',completed_at=?,screenshot_after_id=?,observation_json=? WHERE id=? AND state='dispatched'`,
		formatTime(time.Now().UTC()), observation.ScreenshotArtifactID, string(observationBody), id)
	if err != nil {
		return DesktopAction{}, err
	}
	completed, err := s.GetDesktopAction(context.Background(), id)
	if err == nil {
		completed.Observation = &observation
	}
	return completed, err
}
