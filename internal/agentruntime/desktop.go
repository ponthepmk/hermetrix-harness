package agentruntime

import "time"

// Points use pixels in the captured window image, including its frame.
type DesktopPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}
type DesktopBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
type DesktopWindow struct {
	Handle    int64         `json:"handle"`
	ProcessID int           `json:"process_id"`
	Title     string        `json:"title"`
	Bounds    DesktopBounds `json:"bounds"`
}
type DesktopElement struct {
	Ref          int           `json:"ref"`
	Handle       int64         `json:"handle"`
	Name         string        `json:"name"`
	AutomationID string        `json:"automation_id"`
	ControlType  string        `json:"control_type"`
	NativeClass  string        `json:"native_class,omitempty"`
	Value        string        `json:"value,omitempty"`
	IsPassword   bool          `json:"is_password"`
	Enabled      bool          `json:"enabled"`
	Bounds       DesktopBounds `json:"bounds"`
	Point        *DesktopPoint `json:"point,omitempty"`
}
type DesktopObservation struct {
	Window               DesktopWindow    `json:"window"`
	Elements             []DesktopElement `json:"elements"`
	ScreenshotArtifactID string           `json:"screenshot_artifact_id,omitempty"`
	ScreenshotHash       string           `json:"screenshot_hash,omitempty"`
	ScreenshotError      string           `json:"screenshot_error,omitempty"`
	ObservedAt           time.Time        `json:"observed_at"`
	ObservationHash      string           `json:"observation_hash"`
}
type DesktopActionInput struct {
	TaskID          string        `json:"task_id,omitempty"`
	Handle          int64         `json:"handle"`
	ProcessID       int           `json:"process_id"`
	Action          string        `json:"action"`
	ElementRef      int           `json:"element_ref"`
	Text            string        `json:"text,omitempty"`
	ObservationHash string        `json:"observation_hash"`
	Point           *DesktopPoint `json:"point,omitempty"`
}
type DesktopAction struct {
	ID                 string                       `json:"id"`
	TaskID             string                       `json:"task_id,omitempty"`
	Handle             int64                        `json:"handle"`
	ProcessID          int                          `json:"process_id"`
	Action             string                       `json:"action"`
	ElementRef         int                          `json:"element_ref"`
	Target             DesktopElement               `json:"target"`
	ObservationHash    string                       `json:"observation_hash"`
	SubjectHash        string                       `json:"subject_hash"`
	State              string                       `json:"state"`
	Error              string                       `json:"error,omitempty"`
	CreatedAt          time.Time                    `json:"created_at"`
	ExpiresAt          time.Time                    `json:"expires_at"`
	Observation        *DesktopObservation          `json:"observation,omitempty"`
	Reconciliation     *DesktopActionReconciliation `json:"reconciliation,omitempty"`
	ScreenshotBeforeID string                       `json:"screenshot_before_id,omitempty"`
	ScreenshotAfterID  string                       `json:"screenshot_after_id,omitempty"`
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
