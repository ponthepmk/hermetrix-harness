package skills

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Service) GetBehavioralRun(ctx context.Context, runID string) (BehavioralRun, error) {
	if runID == "" || !isTrimExactUTF8(runID, 160) {
		return BehavioralRun{}, fmt.Errorf("behavioral run id: %w", ErrBehavioralRunBindings)
	}
	owner, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return BehavioralRun{}, err
	}
	run, err := scanBehavioralRun(s.store.DB.QueryRowContext(ctx, behavioralRunSelect+` WHERE id = ? AND owner_principal_id=?`, runID, owner))
	if errors.Is(err, sql.ErrNoRows) {
		return BehavioralRun{}, ErrNotFound
	}
	if err != nil {
		return BehavioralRun{}, err
	}
	return run, nil
}
