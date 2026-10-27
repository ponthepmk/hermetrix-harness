package skills

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func behavioralRunBaseSnapshot(ctx context.Context, tx *sql.Tx, c Candidate, ownerID string) (string, string, error) {
	if c.BaseVersionID == "" && c.TargetSkillID == "" {
		return "", "", nil
	}
	if (c.BaseVersionID == "" && c.TargetSkillID != "") || (c.BaseVersionID != "" && c.TargetSkillID == "") {
		return "", "", fmt.Errorf("revision conflict: exactly one of BaseVersionID and TargetSkillID must be empty: %w", ErrRevisionConflict)
	}
	var contentHash, packageBlobRef string
	row := tx.QueryRowContext(ctx, `SELECT v.content_hash, v.package_blob_ref
		FROM skill_versions v
		JOIN skills s ON s.id = v.skill_id
		WHERE v.id = ? AND s.id = ? AND s.owner_principal_id = ? AND s.current_version_id = v.id
		AND s.state = 'active' AND s.canonical_name = ? AND s.scope_kind = ? AND s.scope_ref = ? AND s.origin = ? AND s.owner = ?`,
		c.BaseVersionID, c.TargetSkillID, ownerID, c.CanonicalName, c.ScopeKind, c.ScopeRef, c.Origin, c.Owner)
	if err := row.Scan(&contentHash, &packageBlobRef); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", fmt.Errorf("no valid owned current base: %w", ErrRevisionConflict)
		}
		return "", "", err
	}
	return contentHash, packageBlobRef, nil
}
