package product

import (
	"strings"
	"testing"
)

// TestExportCarriesOnlyWhatImportRestores closes O-21.
//
// Export used to serialise all 42 tables while import read two. A real restore
// of a file holding 210 events, 4 sessions and 21 blobs produced three Skill
// candidates and nothing else, reporting state: imported and conflicts: 0 --
// true of what it did, misleading about what the file held.
//
// The owner chose to narrow the export. A file carrying conversations, provider
// credentials and approval receipts that nothing reads is a workspace's private
// history travelling somewhere for no purpose.
func TestExportCarriesOnlyWhatImportRestores(t *testing.T) {
	for _, table := range backupTables {
		if !strings.HasPrefix(table, "skill") && !strings.HasPrefix(table, "candidate") {
			t.Fatalf("export carries %q, which is not part of the Skill lifecycle", table)
		}
	}
	// The tables whose contents made the old export a privacy question.
	for _, table := range []string{"agent_events", "agent_sessions", "provider_profiles",
		"tool_approvals", "context_snapshots", "memories", "event_embeddings"} {
		for _, carried := range backupTables {
			if carried == table {
				t.Fatalf("export still carries %q", table)
			}
		}
	}
}
