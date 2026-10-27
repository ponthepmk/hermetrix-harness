package taskcoord

import (
	"encoding/json"
	"errors"
	"time"

	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

type CodingOperationInput struct {
	OperationID string                     `json:"operation_id"`
	Task        taskengine.CreateTaskInput `json:"task"`
	Goal        StartGoalInput             `json:"goal"`
}

type CodingOperation struct {
	ID                  string                `json:"id"`
	OwnerPrincipalID    string                `json:"owner_principal_id"`
	RequestSHA256       string                `json:"request_sha256"`
	TaskID              string                `json:"task_id"`
	GoalID              string                `json:"goal_id"`
	Request             json.RawMessage       `json:"request"`
	InitialTaskRevision int                   `json:"initial_task_revision"`
	GoalConfig          taskengine.GoalConfig `json:"goal_config"`
	CreatedAt           time.Time             `json:"created_at"`
}

type CodingOperationResult struct {
	Operation CodingOperation     `json:"operation"`
	Task      taskengine.Task     `json:"task"`
	Goal      *taskengine.GoalRun `json:"goal,omitempty"`
}

type CodingOperations struct {
	dataStore   *store.Store
	coordinator *Service
}

func NewCodingOperations(dataStore *store.Store, coordinator *Service) *CodingOperations {
	return &CodingOperations{
		dataStore:   dataStore,
		coordinator: coordinator,
	}
}

func (c *CodingOperations) configured() error {
	if c == nil {
		return errors.New("coding operations: receiver is nil")
	}
	if c.dataStore == nil || c.dataStore.DB == nil {
		return errors.New("coding operations: data store is nil")
	}
	if c.coordinator == nil {
		return errors.New("coding operations: coordinator is nil")
	}
	if c.coordinator.tasks == nil {
		return errors.New("coding operations: coordinator tasks service is nil")
	}
	if c.coordinator.products == nil {
		return errors.New("coding operations: coordinator products service is nil")
	}
	if c.coordinator.providers == nil {
		return errors.New("coding operations: coordinator providers service is nil")
	}
	return nil
}
