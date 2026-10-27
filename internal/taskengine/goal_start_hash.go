package taskengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

type goalStartInput struct {
	TaskID               string     `json:"task_id"`
	ExpectedTaskRevision int        `json:"expected_task_revision"`
	Actor                string     `json:"actor"`
	Config               GoalConfig `json:"config"`
}

func goalStartInputSHA256(taskID string, expectedRevision int, actor string, config GoalConfig) (string, error) {
	cfg := config
	cfg.StartInputSHA256 = ""

	if !utf8.ValidString(taskID) ||
		!utf8.ValidString(strings.TrimSpace(actor)) ||
		!utf8.ValidString(cfg.ProjectRoot) ||
		!utf8.ValidString(cfg.ProviderID) ||
		!utf8.ValidString(cfg.ProviderRevision) ||
		!utf8.ValidString(cfg.ReviewerProviderID) ||
		!utf8.ValidString(cfg.ReviewerProviderRevision) {
		return "", errors.New("invalid UTF-8 in goal start hash inputs")
	}

	input := goalStartInput{
		TaskID:               taskID,
		ExpectedTaskRevision: expectedRevision,
		Actor:                strings.TrimSpace(actor),
		Config:               cfg,
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
