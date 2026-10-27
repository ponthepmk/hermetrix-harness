package taskcoord

import "hermetrix-harness/internal/taskengine"

// A worker implements one approved step. Global constraints still apply, but
// future steps' acceptance criteria must not expand the current write scope.
func stepCriteria(packet taskengine.StepPacket) []string {
	ids := make(map[string]bool, len(packet.Step.RequirementIDs))
	for _, id := range packet.Step.RequirementIDs {
		ids[id] = true
	}
	var result []string
	for _, criterion := range packet.Requirement.Criteria {
		if len(ids) == 0 || ids[criterion.ID] {
			result = append(result, criterion.ID+": "+criterion.Description)
		}
	}
	return result
}
