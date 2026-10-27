package taskcoord

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

func canonicalCodingInput(in CodingOperationInput) (CodingOperationInput, []byte, string, error) {
	if in.OperationID == "" {
		return zeroCodingOperationInput(), nil, "", errors.New("operation_id is required")
	}
	if in.OperationID != strings.TrimSpace(in.OperationID) {
		return zeroCodingOperationInput(), nil, "", errors.New("operation_id must not contain leading or trailing whitespace")
	}
	if !utf8.ValidString(in.OperationID) || len(in.OperationID) > 160 {
		return zeroCodingOperationInput(), nil, "", errors.New("operation_id must be valid UTF-8 and at most 160 bytes")
	}
	if in.Task.ProjectID == "" {
		return zeroCodingOperationInput(), nil, "", errors.New("task.project_id is required")
	}
	if in.Task.ProjectID != strings.TrimSpace(in.Task.ProjectID) {
		return zeroCodingOperationInput(), nil, "", errors.New("task.project_id must not contain leading or trailing whitespace")
	}
	if in.Goal.GoalID != "" || in.Goal.TaskID != "" || in.Goal.ExpectedTaskRevision != 0 {
		return zeroCodingOperationInput(), nil, "", errors.New("goal.GoalID, goal.TaskID, and goal.ExpectedTaskRevision must be empty or zero")
	}
	if in.Goal.ApplyChanges && len(in.Task.Contract.WritablePaths) == 0 {
		return zeroCodingOperationInput(), nil, "", errors.New("task.contract.writable_paths is required when apply_changes is true")
	}

	out := in
	out.Task.Actor = strings.TrimSpace(out.Task.Actor)
	out.Task.Title = strings.TrimSpace(out.Task.Title)
	out.Task.Objective = strings.TrimSpace(out.Task.Objective)
	out.Task.ProjectID = strings.TrimSpace(out.Task.ProjectID)
	out.Goal.Actor = strings.TrimSpace(out.Goal.Actor)
	if out.Goal.Actor == "" {
		out.Goal.Actor = out.Task.Actor
	}
	if out.Task.EgressPolicy == "" {
		out.Task.EgressPolicy = "local_only"
	}
	if out.Goal.MaxActions == 0 {
		out.Goal.MaxActions = 100
	}
	if out.Goal.MaxAttempts == 0 {
		out.Goal.MaxAttempts = 8
	}
	if out.Goal.MaxPlans == 0 {
		out.Goal.MaxPlans = 3
	}
	if out.Goal.MaxSeconds == 0 {
		out.Goal.MaxSeconds = 3600
	}
	if out.Goal.VerificationTimeoutSeconds == 0 {
		out.Goal.VerificationTimeoutSeconds = 120
	}

	if !codingStringsValid(reflect.ValueOf(out), 0) {
		return zeroCodingOperationInput(), nil, "", errors.New("normalized input contains invalid UTF-8 strings")
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return zeroCodingOperationInput(), nil, "", err
	}
	if len(raw) > 64*1024 {
		return zeroCodingOperationInput(), nil, "", errors.New("normalized input exceeds 64KiB")
	}
	hash := sha256.Sum256(raw)
	return out, raw, fmt.Sprintf("%x", hash), nil
}

func zeroCodingOperationInput() CodingOperationInput {
	return CodingOperationInput{}
}
