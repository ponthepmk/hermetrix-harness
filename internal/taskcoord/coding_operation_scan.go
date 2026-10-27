package taskcoord

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func scanCodingOperation(row interface{ Scan(...any) error }) (CodingOperation, error) {
	var op CodingOperation
	var rawReq, rawCfg, rawTime string
	if err := row.Scan(&op.ID, &op.OwnerPrincipalID, &op.RequestSHA256, &rawReq, &op.TaskID, &op.GoalID, &op.InitialTaskRevision, &rawCfg, &rawTime); err != nil {
		return CodingOperation{}, err
	}
	var in CodingOperationInput
	if err := json.Unmarshal([]byte(rawReq), &in); err != nil {
		return CodingOperation{}, err
	}
	normalized, canonicalBytes, digest, err := canonicalCodingInput(in)
	if err != nil {
		return CodingOperation{}, err
	}
	if !bytes.Equal(canonicalBytes, []byte(rawReq)) || digest != op.RequestSHA256 || normalized.OperationID != op.ID || codingGoalID(op.OwnerPrincipalID, op.ID) != op.GoalID {
		return CodingOperation{}, errors.New("coding operation: invalid scan")
	}
	if op.InitialTaskRevision <= 0 {
		return CodingOperation{}, errors.New("coding operation: initial task revision must be positive")
	}
	var cfg taskengine.GoalConfig
	if err := json.Unmarshal([]byte(rawCfg), &cfg); err != nil {
		return CodingOperation{}, err
	}
	if cfg.StartInputSHA256 != "" {
		return CodingOperation{}, errors.New("coding operation: goal config marker must be empty")
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return CodingOperation{}, err
	}
	if !bytes.Equal(cfgBytes, []byte(rawCfg)) {
		return CodingOperation{}, errors.New("coding operation: goal config bytes mismatch")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, rawTime)
	if err != nil {
		return CodingOperation{}, err
	}
	op.CreatedAt = createdAt
	op.Request = json.RawMessage([]byte(rawReq))
	op.GoalConfig = cfg
	return op, nil
}
