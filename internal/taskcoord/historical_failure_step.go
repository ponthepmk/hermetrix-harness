package taskcoord

import (
	"strconv"
	"strings"

	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

// historicalFailureStepID returns the step ID to use when replanning from a
// historical failure. It defaults to the current packet's step ID. A cross-step
// origin is selected only when the supplied evidence and packet validation
// evidence unambiguously identify a single matching saved validation record.
func historicalFailureStepID(packet taskengine.StepPacket, evidence worker.FailureEvidence) string {
	defaultStepID := packet.Step.ID

	if packet.TaskID == "" || evidence.ValidationID == "" || evidence.ArtifactID == "" ||
		evidence.CheckID == "" || evidence.SubjectRevision == "" {
		return defaultStepID
	}

	if !contains(packet.Step.Checks, evidence.CheckID) {
		return defaultStepID
	}

	var selected string
	for _, record := range packet.ValidationEvidence {
		if !matchesEvidenceRecord(record, packet, evidence) {
			continue
		}
		if selected != "" {
			// Duplicate eligible records are ambiguous even if identical.
			return defaultStepID
		}
		if !isCanonicalSubjectRevision(record.StepID, record.SubjectRevision) {
			return defaultStepID
		}
		selected = record.StepID
	}

	if selected == "" {
		return defaultStepID
	}

	return selected
}

func matchesEvidenceRecord(record taskengine.Validation, packet taskengine.StepPacket, evidence worker.FailureEvidence) bool {
	if record.ID != evidence.ValidationID {
		return false
	}
	if record.TaskID != packet.TaskID {
		return false
	}
	if record.Status != taskengine.ValidationFail {
		return false
	}
	if record.StepID == "" {
		return false
	}
	if record.CheckID != evidence.CheckID {
		return false
	}
	if record.SubjectRevision != evidence.SubjectRevision {
		return false
	}
	if record.Actual != evidence.Actual {
		return false
	}
	if !contains(record.EvidenceRefs, "artifact:"+evidence.ArtifactID) {
		return false
	}
	return true
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func isCanonicalSubjectRevision(stepID, subjectRevision string) bool {
	prefix := "step:" + stepID + ":r"
	if !strings.HasPrefix(subjectRevision, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(subjectRevision, prefix)
	n, err := strconv.Atoi(suffix)
	if err != nil || n <= 0 {
		return false
	}
	canonical := taskengine.StepSubjectRevision(taskengine.Step{ID: stepID, Revision: n})
	return canonical == subjectRevision
}
