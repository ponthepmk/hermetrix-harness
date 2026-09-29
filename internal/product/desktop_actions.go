package product

import "time"

type DesktopActionInput struct {
	TaskID          string `json:"task_id,omitempty"`
	Handle          int64  `json:"handle"`
	ProcessID       int    `json:"process_id"`
	Action          string `json:"action"`
	ElementRef      int    `json:"element_ref"`
	Text            string `json:"text,omitempty"`
	ObservationHash string `json:"observation_hash"`
}

type DesktopAction struct {
	ID              string                       `json:"id"`
	TaskID          string                       `json:"task_id,omitempty"`
	Handle          int64                        `json:"handle"`
	ProcessID       int                          `json:"process_id"`
	Action          string                       `json:"action"`
	ElementRef      int                          `json:"element_ref"`
	Target          DesktopElement               `json:"target"`
	ObservationHash string                       `json:"observation_hash"`
	SubjectHash     string                       `json:"subject_hash"`
	State           string                       `json:"state"`
	Error           string                       `json:"error,omitempty"`
	CreatedAt       time.Time                    `json:"created_at"`
	ExpiresAt       time.Time                    `json:"expires_at"`
	Observation     *DesktopObservation          `json:"observation,omitempty"`
	Reconciliation  *DesktopActionReconciliation `json:"reconciliation,omitempty"`
}

type DesktopActionReconciliation struct {
	ActionID             string    `json:"action_id"`
	Resolution           string    `json:"resolution"`
	ObservationHash      string    `json:"observation_hash"`
	ScreenshotArtifactID string    `json:"screenshot_artifact_id"`
	Actor                string    `json:"actor"`
	Note                 string    `json:"note"`
	CreatedAt            time.Time `json:"created_at"`
}
