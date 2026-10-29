package taskcoord

import (
	"encoding/json"
	"fmt"
	"slices"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/worker"
)

const verificationScopeVersion = 1

// The marker belongs to verification evidence, not model instructions. Zero
// preserves historical snapshots; version one binds ALL selected source bytes.
type scopedVerificationBundle struct {
	verificationBundle
	ScopeVersion int `json:"scope_version,omitempty"`
}

func verificationSourceHashes(loaded loadedProposal) map[string]string {
	if loaded.InputTask == nil {
		return nil
	}
	expected := make(map[string]string, len(loaded.InputTask.Files))
	for path, content := range loaded.InputTask.Files {
		expected[path] = worker.Hash(content)
		if contains(loaded.InputTask.AbsentFiles, path) {
			expected[path] = "absent"
		}
	}
	for _, change := range loaded.Bundle.Result.Changes {
		expected[change.Path] = worker.Hash(change.Content)
	}
	return expected
}

func validateVerificationScope(loaded loadedProposal, snapshot product.ProjectSnapshot, version int) error {
	if version < 0 || version > verificationScopeVersion {
		return fmt.Errorf("unsupported verification scope recipe")
	}
	if version < loaded.Bundle.VerificationScopeVersion {
		return fmt.Errorf("verification scope was downgraded below its immutable proposal policy")
	}
	if loaded.Bundle.Reduction != nil && loaded.Bundle.Reduction.Version == 2 && version != verificationScopeVersion {
		return fmt.Errorf("reduction requires full selected-source verification scope")
	}
	if version == 0 {
		return nil
	}
	expected := verificationSourceHashes(loaded)
	if len(expected) == 0 || snapshot.Root != loaded.Artifact.Metadata["project_root"] {
		return fmt.Errorf("verification scope lacks frozen source/root")
	}
	for path, hash := range expected {
		if snapshot.Hashes[path] != hash {
			return fmt.Errorf("verification scope does not cover selected source %q", path)
		}
	}
	return nil
}

func validateStoredVerificationScope(loaded loadedProposal, body []byte, snapshot product.ProjectSnapshot) error {
	var policy struct {
		ScopeVersion int `json:"scope_version"`
	}
	if err := json.Unmarshal(body, &policy); err != nil {
		return fmt.Errorf("invalid verification scope marker")
	}
	return validateVerificationScope(loaded, snapshot, policy.ScopeVersion)
}

// Only a known readonly SHA assertion can be covered by the full selected-source
// guard. Unknown paths/hashes, writable files, contains assertions, and behavioral
// commands stay in the exact ordered comparison. Legacy proof v1 stays strict.
func reductionChecksCompatible(prior, current []string, task worker.Task, contractWrites []string, version int) bool {
	if version == 1 {
		return slices.Equal(prior, current)
	}
	if version != 2 {
		return false
	}
	filter := func(checks []string) []string {
		out := make([]string, 0, len(checks))
		for _, check := range checks {
			argv, err := parseFrozenCommand(check)
			covered := false
			if err == nil && len(argv) == 3 && argv[0] == "assert-file-sha256" {
				content, selected := task.Files[argv[1]]
				readonly := (len(task.WritablePaths) > 0 && !contains(task.WritablePaths, argv[1])) || (len(contractWrites) > 0 && !contains(contractWrites, argv[1]))
				_, assertionErr := compileFileAssertion(argv)
				covered = assertionErr == nil && selected && readonly && argv[2] == worker.Hash(content)
			}
			if !covered {
				out = append(out, check)
			}
		}
		return out
	}
	a, b := filter(prior), filter(current)
	return len(a) > 0 && slices.Equal(a, b)
}
