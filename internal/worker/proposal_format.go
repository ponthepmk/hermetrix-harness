package worker

// validateProposalFormat checks that task.ProposalFormat is a known mode and
// that the mode is compatible with the task's absent-file list.
func validateProposalFormat(task Task) error {
	switch task.ProposalFormat {
	case "", "edits_only", "content_only":
		// valid
	default:
		return workerError(ErrorInvalidTask, PhaseValidatingInput, false, "unsupported proposal_format", nil)
	}
	if task.ProposalFormat == "edits_only" && len(task.AbsentFiles) > 0 {
		return workerError(ErrorInvalidTask, PhaseValidatingInput, false, "edits_only cannot create absent files", nil)
	}
	return nil
}

// proposalSchemaForFormat returns the tool-parameter schema for the given
// proposal format. Blank returns the unchanged legacy schema. edits_only
// removes the content property from the change item and requires path+edits.
// content_only removes the edits property and requires path+content.
func proposalSchemaForFormat(format string) map[string]any {
	schema := proposalSchema()
	if format == "" {
		return schema
	}
	props := schema["properties"].(map[string]any)
	changes := props["changes"].(map[string]any)
	items := changes["items"].(map[string]any)
	changeProps := items["properties"].(map[string]any)
	switch format {
	case "edits_only":
		delete(changeProps, "content")
		items["required"] = []string{"path", "edits"}
	case "content_only":
		delete(changeProps, "edits")
		items["required"] = []string{"path", "content"}
	}
	return schema
}

// proposalFormatInstruction returns a fixed instruction string for the given
// format. Blank returns "" so the legacy system prompt is unchanged.
func proposalFormatInstruction(format string) string {
	switch format {
	case "":
		return ""
	case "edits_only":
		return "You must use edits_only mode. Every change MUST contain path and a non-empty edits array of exact old/new text replacements. Do NOT include a content field."
	case "content_only":
		return "You must use content_only mode. Every change MUST contain path and a content field with the complete replacement file text. Do NOT include an edits field."
	default:
		return ""
	}
}

// validateProposalRepresentation enforces the representation constraints for
// the given proposal format on a single change. Legacy (blank) defers to the
// existing materializeChange XOR check. Unsupported modes reject.
func validateProposalRepresentation(format string, content *string, edits []TextEdit) error {
	switch format {
	case "":
		return nil
	case "edits_only":
		if content != nil {
			return invalidProposal("edits_only mode requires nil content")
		}
		if len(edits) == 0 {
			return invalidProposal("edits_only mode requires a non-empty edits array")
		}
		return nil
	case "content_only":
		if content == nil {
			return invalidProposal("content_only mode requires non-nil content")
		}
		if len(edits) > 0 {
			return invalidProposal("content_only mode must not include edits")
		}
		return nil
	default:
		return invalidProposal("unsupported proposal format")
	}
}

// proposalFormatAllowsStrategy reports whether the given strategy is permitted
// under the given proposal format. absent indicates the file is in
// task.AbsentFiles. Legacy (blank) always returns true so existing strategy
// guards continue to run. Unknown formats return false.
func proposalFormatAllowsStrategy(format string, absent bool, strategy string) bool {
	switch format {
	case "":
		return true
	case "edits_only":
		return !absent && strategy == "replace_text"
	case "content_only":
		if absent {
			return strategy == "create_file"
		}
		return strategy == "replace_file"
	default:
		return false
	}
}
