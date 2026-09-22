package product

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hermetrix-harness/internal/identity"
)

const (
	mutationPlanned    = "planned"
	mutationDispatched = "dispatched"
	mutationObserved   = "observed"
	mutationReconciled = "reconciled"
	mutationAbandoned  = "abandoned"
	mutationUncertain  = "uncertain"
)

func (s *Service) createFileMutationIntent(ctx context.Context, projectID, path, actor, beforeSHA, afterSHA string) (FileMutationIntent, error) {
	now := time.Now().UTC()
	item := FileMutationIntent{ID: identity.New("mutation"), OperationID: identity.New("operation"), ProjectID: projectID,
		Path: path, Actor: actor, BeforeSHA256: beforeSHA, AfterSHA256: afterSHA, State: mutationPlanned,
		CreatedAt: now, UpdatedAt: now}
	_, err := s.store.DB.ExecContext(ctx, `INSERT INTO file_mutation_intents(
		id,operation_id,project_id,path,actor,before_sha256,after_sha256,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,'planned',?,?)`, item.ID, item.OperationID, item.ProjectID, item.Path, item.Actor,
		item.BeforeSHA256, item.AfterSHA256, formatTime(now), formatTime(now))
	return item, err
}

func (s *Service) transitionFileMutation(ctx context.Context, operationID, from, to, receiptArtifactID, message string) error {
	result, err := s.store.DB.ExecContext(ctx, `UPDATE file_mutation_intents SET state=?,receipt_artifact_id=?,error=?,updated_at=?
		WHERE operation_id=? AND state=?`, to, nullIfEmpty(receiptArtifactID), message, formatTime(time.Now().UTC()), operationID, from)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("file mutation transition %s to %s is stale", from, to)
	}
	return nil
}

func (s *Service) RecoverFileMutations(ctx context.Context) (int64, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT operation_id,project_id,path,actor,before_sha256,after_sha256
		FROM file_mutation_intents WHERE state='dispatched' ORDER BY created_at`)
	if err != nil {
		return 0, err
	}
	type pending struct{ operationID, projectID, path, actor, beforeSHA, afterSHA string }
	items := []pending{}
	for rows.Next() {
		var item pending
		if err = rows.Scan(&item.operationID, &item.projectID, &item.path, &item.actor, &item.beforeSHA, &item.afterSHA); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	var recovered int64
	for _, item := range items {
		project, projectErr := s.GetProject(ctx, item.projectID)
		if projectErr != nil {
			_ = s.transitionFileMutation(ctx, item.operationID, mutationDispatched, mutationUncertain, "", "project unavailable during recovery")
			continue
		}
		root, rootErr := requireRoot(project)
		if rootErr != nil {
			_ = s.transitionFileMutation(ctx, item.operationID, mutationDispatched, mutationUncertain, "", "project root unavailable during recovery")
			continue
		}
		path, _, pathErr := regularProjectFile(root, item.path)
		current := "absent"
		if pathErr == nil {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				_ = s.transitionFileMutation(ctx, item.operationID, mutationDispatched, mutationUncertain, "", "file unreadable during recovery")
				continue
			}
			current = hashBytes(data)
		} else if !errors.Is(pathErr, os.ErrNotExist) {
			_ = s.transitionFileMutation(ctx, item.operationID, mutationDispatched, mutationUncertain, "", "path changed during recovery")
			continue
		}
		switch current {
		case item.afterSHA:
			artifact, _, findErr := s.FindArtifactByOperationID(ctx, item.operationID)
			if findErr != nil && !errors.Is(findErr, sql.ErrNoRows) {
				return recovered, findErr
			}
			if errors.Is(findErr, sql.ErrNoRows) {
				body, _ := json.Marshal(map[string]any{"operation_id": item.operationID, "path": item.path,
					"before_sha256": item.beforeSHA, "after_sha256": item.afterSHA, "recovered": true})
				artifact, findErr = s.CreateArtifact(ctx, ArtifactInput{ProjectID: item.projectID,
					Name: filepath.Base(item.path) + ".mutation-receipt.json", Kind: "file_mutation_receipt",
					MIMEType: "application/vnd.hermetrix.file-mutation+json", Content: string(body),
					Metadata: map[string]any{"operation_id": item.operationID, "path": item.path, "recovered": true}})
				if findErr != nil {
					return recovered, findErr
				}
			}
			if err = s.transitionFileMutation(ctx, item.operationID, mutationDispatched, mutationReconciled, artifact.ID, ""); err != nil {
				return recovered, err
			}
		case item.beforeSHA:
			if err = s.transitionFileMutation(ctx, item.operationID, mutationDispatched, mutationAbandoned, "", "replacement did not commit"); err != nil {
				return recovered, err
			}
		default:
			if err = s.transitionFileMutation(ctx, item.operationID, mutationDispatched, mutationUncertain, "", "file hash matches neither mutation boundary"); err != nil {
				return recovered, err
			}
		}
		recovered++
	}
	return recovered, nil
}
