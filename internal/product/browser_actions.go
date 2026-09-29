package product

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

func (s *Service) PrepareBrowserAction(ctx context.Context, tabID string, input BrowserActionInput) (BrowserActionReceipt, error) {
	if input.Action != "click" && input.Action != "type" && input.Action != "key" {
		return BrowserActionReceipt{}, fmt.Errorf("only browser click, type and key require preparation")
	}
	if input.Ref < 1 || input.ObservationHash == "" || strings.TrimSpace(input.Actor) == "" || len(input.Text) > 64<<10 ||
		(input.Action == "click" && input.Text != "") || (input.Action == "key" && !supportedBrowserKey(input.Text)) {
		return BrowserActionReceipt{}, fmt.Errorf("browser action requires an exact element, observation and actor")
	}
	runtimeTab, err := s.liveBrowserTab(tabID)
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	runtimeTab.mu.Lock()
	defer runtimeTab.mu.Unlock()
	if input.ObservationHash != runtimeTab.tab.ObservationHash {
		return BrowserActionReceipt{}, fmt.Errorf("browser observation changed; read the page again")
	}
	if err := s.refreshBrowserTab(ctx, runtimeTab, true); err != nil {
		return BrowserActionReceipt{}, err
	}
	if input.ObservationHash != runtimeTab.tab.ObservationHash || runtimeTab.tab.ScreenshotArtifactID == "" {
		_ = s.persistBrowserTab(ctx, runtimeTab.tab)
		return BrowserActionReceipt{}, fmt.Errorf("browser observation changed or screenshot is unavailable; read the page again")
	}
	if err := s.persistBrowserTab(ctx, runtimeTab.tab); err != nil {
		return BrowserActionReceipt{}, err
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	if input.TaskID != "" {
		taskOwner, ownerErr := s.researchTaskOwner(ctx, input.TaskID)
		var taskProject string
		projectErr := s.store.DB.QueryRowContext(ctx, `SELECT project_id FROM durable_tasks WHERE id=?`, input.TaskID).Scan(&taskProject)
		if ownerErr != nil || projectErr != nil || taskOwner != ownerID || taskProject != runtimeTab.tab.ProjectID {
			return BrowserActionReceipt{}, fmt.Errorf("browser action Task is unavailable for this tab")
		}
	}
	var unresolved int
	_, _ = s.store.DB.ExecContext(ctx, `UPDATE browser_action_receipts SET state='cancelled',completed_at=? WHERE owner_principal_id=? AND tab_id=? AND state='prepared' AND expires_at<=?`,
		formatTime(time.Now().UTC()), ownerID, tabID, formatTime(time.Now().UTC()))
	if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM browser_action_receipts a WHERE a.owner_principal_id=? AND a.tab_id=?
		AND (a.state IN ('prepared','dispatched') OR (a.state='uncertain' AND NOT EXISTS
		(SELECT 1 FROM browser_action_reconciliations r WHERE r.action_id=a.id)))`, ownerID, tabID).Scan(&unresolved); err != nil {
		return BrowserActionReceipt{}, err
	}
	if unresolved > 0 {
		return BrowserActionReceipt{}, fmt.Errorf("a browser action for this tab is unresolved; cancel or reconcile it first")
	}
	var target BrowserElement
	for _, element := range runtimeTab.tab.Elements {
		if element.Ref == input.Ref {
			target = element
			break
		}
	}
	if target.Ref == 0 {
		return BrowserActionReceipt{}, fmt.Errorf("browser element reference is stale")
	}
	if target.IsPassword {
		return BrowserActionReceipt{}, fmt.Errorf("password fields are unavailable to managed browser actions")
	}
	textDigest := sha256.Sum256([]byte(input.Text))
	textHash := hex.EncodeToString(textDigest[:])
	subject, _ := json.Marshal(struct {
		Owner, Task, Tab, Action string
		Ref                      int
		Target                   BrowserElement
		TextHash, Observation    string
	}{
		ownerID, input.TaskID, tabID, input.Action, input.Ref, target, textHash, input.ObservationHash})
	sum := sha256.Sum256(subject)
	now := time.Now().UTC()
	item := BrowserActionReceipt{ID: identity.New("browseraction"), TaskID: input.TaskID, TabID: tabID,
		Action: input.Action, ElementRef: input.Ref, Target: target, ObservationHash: input.ObservationHash,
		SubjectHash: hex.EncodeToString(sum[:]), ScreenshotBeforeID: runtimeTab.tab.ScreenshotArtifactID,
		State: "prepared", CreatedAt: now, ExpiresAt: now.Add(2 * time.Minute)}
	targetJSON, _ := json.Marshal(target)
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO browser_action_receipts(id,owner_principal_id,task_id,tab_id,action,element_ref,
		target_json,text_hash,observation_hash,subject_hash,screenshot_before_id,state,created_at,expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'prepared',?,?)`, item.ID, ownerID, item.TaskID, tabID, item.Action, item.ElementRef,
		string(targetJSON), textHash, item.ObservationHash, item.SubjectHash, item.ScreenshotBeforeID, formatTime(now), formatTime(item.ExpiresAt))
	return item, err
}

func (s *Service) GetBrowserActionReceipt(ctx context.Context, id string) (BrowserActionReceipt, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	var item BrowserActionReceipt
	var targetJSON, created, expires string
	err = s.store.DB.QueryRowContext(ctx, `SELECT id,task_id,tab_id,action,element_ref,target_json,observation_hash,subject_hash,
		screenshot_before_id,screenshot_after_id,state,error,created_at,expires_at FROM browser_action_receipts WHERE id=? AND owner_principal_id=?`, id, ownerID).
		Scan(&item.ID, &item.TaskID, &item.TabID, &item.Action, &item.ElementRef, &targetJSON, &item.ObservationHash, &item.SubjectHash,
			&item.ScreenshotBeforeID, &item.ScreenshotAfterID, &item.State, &item.Error, &created, &expires)
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	if err := json.Unmarshal([]byte(targetJSON), &item.Target); err != nil {
		return BrowserActionReceipt{}, err
	}
	item.CreatedAt, _ = parseTime(created)
	item.ExpiresAt, _ = parseTime(expires)
	var reconciliation BrowserActionReconciliation
	var reconciledAt string
	err = s.store.DB.QueryRowContext(ctx, `SELECT action_id,verification_tab_id,resolution,observation_hash,screenshot_artifact_id,actor,note,created_at
		FROM browser_action_reconciliations WHERE action_id=? AND owner_principal_id=?`, id, ownerID).
		Scan(&reconciliation.ActionID, &reconciliation.VerificationTabID, &reconciliation.Resolution, &reconciliation.ObservationHash, &reconciliation.ScreenshotArtifactID,
			&reconciliation.Actor, &reconciliation.Note, &reconciledAt)
	if err != nil && err != sql.ErrNoRows {
		return BrowserActionReceipt{}, err
	}
	if err == nil {
		reconciliation.CreatedAt, _ = parseTime(reconciledAt)
		item.Reconciliation = &reconciliation
	}
	return item, nil
}

func (s *Service) ListBrowserActionReceipts(ctx context.Context, taskID string) ([]BrowserActionReceipt, error) {
	ownerID, err := s.researchTaskOwner(ctx, taskID)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id FROM browser_action_receipts WHERE task_id=? AND owner_principal_id=? ORDER BY created_at DESC LIMIT 100`, taskID, ownerID)
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
	items := make([]BrowserActionReceipt, 0, len(ids))
	for _, id := range ids {
		item, err := s.GetBrowserActionReceipt(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) CancelBrowserAction(ctx context.Context, id, subjectHash string) (BrowserActionReceipt, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	result, err := s.store.DB.ExecContext(ctx, `UPDATE browser_action_receipts SET state='cancelled',completed_at=? WHERE id=? AND owner_principal_id=? AND subject_hash=? AND state='prepared'`,
		formatTime(time.Now().UTC()), id, ownerID, subjectHash)
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return BrowserActionReceipt{}, fmt.Errorf("browser action is stale or already dispatched")
	}
	return s.GetBrowserActionReceipt(ctx, id)
}

func (s *Service) ReconcileBrowserAction(ctx context.Context, id, verificationTabID, resolution, observationHash, actor, note string) (BrowserActionReceipt, error) {
	if resolution != "observed_applied" && resolution != "observed_not_applied" {
		return BrowserActionReceipt{}, fmt.Errorf("browser reconciliation requires an observed outcome")
	}
	actor, note = strings.TrimSpace(actor), strings.TrimSpace(note)
	if actor == "" || len(note) < 8 || len(note) > 2000 || observationHash == "" {
		return BrowserActionReceipt{}, fmt.Errorf("browser reconciliation requires an actor, observation and description")
	}
	item, err := s.GetBrowserActionReceipt(ctx, id)
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	if item.State != "uncertain" || item.Reconciliation != nil {
		return BrowserActionReceipt{}, fmt.Errorf("browser action is not awaiting reconciliation")
	}
	if verificationTabID == "" {
		verificationTabID = item.TabID
	}
	var originalProject sql.NullString
	var originalURL string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT project_id,url FROM browser_tabs WHERE id=?`, item.TabID).Scan(&originalProject, &originalURL); err != nil {
		return BrowserActionReceipt{}, err
	}
	runtimeTab, err := s.liveBrowserTab(verificationTabID)
	if err != nil {
		return BrowserActionReceipt{}, fmt.Errorf("open a managed tab at the same site and inspect the remote result before reconciliation: %w", err)
	}
	runtimeTab.mu.Lock()
	defer runtimeTab.mu.Unlock()
	beforeURL, beforeErr := url.Parse(originalURL)
	currentURL, currentErr := url.Parse(runtimeTab.tab.URL)
	if beforeErr != nil || currentErr != nil || originalProject.String != runtimeTab.tab.ProjectID ||
		!strings.EqualFold(beforeURL.Scheme, currentURL.Scheme) || !strings.EqualFold(beforeURL.Host, currentURL.Host) {
		return BrowserActionReceipt{}, fmt.Errorf("verification tab must belong to the same project and site")
	}
	if err := s.refreshBrowserTab(ctx, runtimeTab, true); err != nil {
		return BrowserActionReceipt{}, err
	}
	if runtimeTab.tab.ObservationHash != observationHash || runtimeTab.tab.ScreenshotArtifactID == "" {
		return BrowserActionReceipt{}, fmt.Errorf("browser observation changed or screenshot is unavailable; inspect again")
	}
	if err := s.persistBrowserTab(ctx, runtimeTab.tab); err != nil {
		return BrowserActionReceipt{}, err
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO browser_action_reconciliations(action_id,owner_principal_id,verification_tab_id,resolution,observation_hash,
		screenshot_artifact_id,actor,note,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, ownerID, verificationTabID, resolution, observationHash,
		runtimeTab.tab.ScreenshotArtifactID, actor, note, formatTime(time.Now().UTC()))
	if err != nil {
		return BrowserActionReceipt{}, err
	}
	return s.GetBrowserActionReceipt(ctx, id)
}
