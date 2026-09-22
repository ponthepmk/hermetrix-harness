package agentplatform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

// Service is the fixture-only managed-agent adapter. It has task observation
// and persistence capabilities, but no executor, provider, command, MCP, or
// workspace mutation dependency.
type Service struct {
	store     *store.Store
	tasks     *taskengine.Service
	trust     Trust
	mu        sync.Mutex
	decisions atomic.Int64
}

func New(dataStore *store.Store, tasks *taskengine.Service, trust Trust) (*Service, error) {
	if dataStore == nil || tasks == nil || trust.PlatformID == "" || trust.NodeID == "" || trust.AgentID == "" {
		return nil, ErrValidation
	}
	marker := filepath.Join(filepath.Dir(dataStore.Root), ".hermetrix-h1-fixture")
	if info, err := os.Stat(marker); err != nil || info.IsDir() {
		return nil, fmt.Errorf("%w: fixture marker is required", ErrUnsupported)
	}
	if err := installFixtureGuards(context.Background(), dataStore); err != nil {
		return nil, err
	}
	return &Service{store: dataStore, tasks: tasks, trust: trust}, nil
}

func (s *Service) DecisionInvocationCount() int64 { return s.decisions.Load() }

func (s *Service) InvocationCounters() InvocationCounters {
	return InvocationCounters{DecisionSelection: s.decisions.Load()}
}

func installFixtureGuards(ctx context.Context, dataStore *store.Store) error {
	_, err := dataStore.DB.ExecContext(ctx, `
		CREATE TRIGGER IF NOT EXISTS h1_forbid_task_runs BEFORE INSERT ON task_runs
		BEGIN SELECT RAISE(ABORT,'H1 forbids task runs'); END;
		CREATE TRIGGER IF NOT EXISTS h1_forbid_task_attempts BEFORE INSERT ON task_step_attempts
		BEGIN SELECT RAISE(ABORT,'H1 forbids step attempts'); END;
		CREATE TRIGGER IF NOT EXISTS h1_forbid_task_effects BEFORE INSERT ON task_effect_intents
		BEGIN SELECT RAISE(ABORT,'H1 forbids effects'); END;
		CREATE TRIGGER IF NOT EXISTS h1_forbid_background_jobs BEFORE INSERT ON background_jobs
		BEGIN SELECT RAISE(ABORT,'H1 forbids background jobs'); END;
		CREATE TRIGGER IF NOT EXISTS h1_forbid_file_mutations BEFORE INSERT ON file_mutation_intents
		BEGIN SELECT RAISE(ABORT,'H1 forbids workspace mutations'); END;`)
	return err
}
