// Package worker provides a proposal-only external model boundary. It never
// reads a workspace, executes commands, or applies model-generated changes.
package worker

// BoundedReviewRegion is a read-only review evidence region. It carries no
// edit authority and is not a complete file.
type BoundedReviewRegion struct {
	Before       BoundedFileView `json:"before"`
	AfterContent string          `json:"after_content"`
}

// BoundedReviewFile is a read-only review evidence file. It carries no edit
// authority and is not a complete file.
type BoundedReviewFile struct {
	BeforeSHA256 string                `json:"before_sha256"`
	AfterSHA256  string                `json:"after_sha256"`
	Regions      []BoundedReviewRegion `json:"regions"`
}
