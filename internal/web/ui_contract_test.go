package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestHermetrixCockpitExposesEveryNativeWorkbenchRoom(t *testing.T) {
	index := mustUIFile(t, "ui/index.html")
	javascript := mustUIFile(t, "ui/app.js")
	stylesheet := mustUIFile(t, "ui/style.css")
	for _, marker := range []string{
		`id="sessionDock"`, `id="workbenchContent"`, `data-door="assistant"`, `data-door="code"`,
		`data-door="team"`, `data-workbench="review"`, `data-workbench="files"`,
		`data-workbench="terminal"`, `data-workbench="browser"`, `data-workbench="artifacts"`,
		`data-workbench="team"`, `id="commandButton"`, `id="commandDialog"`, `id="capabilityDialog"`,
	} {
		if !strings.Contains(index, marker) {
			t.Errorf("cockpit HTML is missing %s", marker)
		}
	}
	for _, marker := range []string{
		`/api/projects/`, `/api/terminals`, `/api/browser/tabs`, `/api/deliverables`, `/api/teams`,
		`/api/team-runs`, `renderWorkbenchFiles`, `renderWorkbenchTerminal`, `renderWorkbenchBrowser`,
		`renderWorkbenchArtifacts`, `renderWorkbenchTeam`, `resizeWorkbenchTerminal`,
		`renderDeliverableDraftPreview`, `data-team-member`, `data-team-task`, `/cancel`, `cancelWorkbenchTeamRun`,
		`data-team-approval`, `/approval`, `decideWorkbenchTeamApproval`,
		// Command palette, capability picker, paired tool receipts and density.
		`openCommandPalette`, `openCapabilityPicker`, `mentionSkill`, `mentionCapability`, `groupTimeline`, `applyDensity`,
		`data-use-capability`, `tool_search`, `tool_describe`, `tool_call`, `session-contract-panel`,
		// A credential can be typed in and changed from the UI, and starting a
		// session is one button rather than three dropdowns.
		`setProviderCredential`, `setMCPCredential`, `/credential`, `sessionOptions`, `sessionReady`,
		// Configuration is a room of its own, reached from the rail and never
		// from a tab strip beside the conversation.
		`CONFIG_SECTIONS`, `renderConfigNav`, `openConfig`, `closeConfig`, `data-config-page`,
		// An agent-written Skill is reviewable and undoable from the row itself.
		`data-revert-promotion`, `promoted by agent`,
		// A tool server can stop mid call to ask the user something.
		`/api/elicitations`, `elicitationCardHTML`, `pollElicitations`, `data-elicit-accept`,
		// A folder is chosen by browsing, and a session can be deleted.
		`/api/filesystem/directories`, `openFolderPicker`, `data-folder`, `data-delete-session`,
		// Enter sends, and the draft survives the re-render each streamed token
		// causes. Both were regressions waiting to happen in one function.
		`bindComposer`, `captureComposer`, `event.shiftKey`, `draftMessage`,
	} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("workbench JavaScript is missing %s", marker)
		}
	}
	// Pane widths are density tokens, not literals: the same three-pane layout
	// has to fit a 13" laptop and a desktop display without a second stylesheet.
	for _, marker := range []string{
		`grid-template-columns: var(--rail-width)`, `--rail-width:`, `[data-density="compact"]`,
		`.workbench-tabs`, `.reading-card`, `.config-nav-item`, `.config-pane`, `.command-dialog`,
		`.capability-dialog`, `details.tool-receipt`, `.tool-center-grid`,
	} {
		if !strings.Contains(stylesheet, marker) {
			t.Fatalf("cockpit stylesheet no longer defines the three-pane reading-card layout: missing %s", marker)
		}
	}
	if strings.Contains(index, "Aetox") || strings.Contains(index, "../Aetox") || strings.Contains(stylesheet, "../Aetox") {
		t.Fatal("product UI copied or exposed Aetox branding instead of Hermetrix identity")
	}
	if strings.Contains(javascript, ".style.") || strings.Contains(index, `style=`) {
		t.Fatal("UI reintroduced inline style mutations that violate its own CSP")
	}
}

func mustUIFile(t *testing.T, name string) string {
	t.Helper()
	data, err := fs.ReadFile(uiFiles, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestEveryCockpitElementIDIsWiredToBehaviour catches markup that ships without
// the code that drives it. The command palette and the capability picker were
// both shipped as HTML and CSS with no JavaScript at all: the palette button
// did nothing and every capability entry point threw ReferenceError, because
// nothing checked that an id someone put in the document was ever selected.
func TestEveryCockpitElementIDIsWiredToBehaviour(t *testing.T) {
	index := mustUIFile(t, "ui/index.html")
	javascript := mustUIFile(t, "ui/app.js")
	// Presentation-only ids belong here with the reason they need no handler.
	presentationOnly := map[string]string{}
	for _, match := range regexp.MustCompile(`id="([A-Za-z][\w-]*)"`).FindAllStringSubmatch(index, -1) {
		id := match[1]
		if _, allowed := presentationOnly[id]; allowed {
			continue
		}
		if !strings.Contains(javascript, "#"+id) {
			t.Errorf("index.html declares id %q but app.js never selects #%s: the element is dead markup", id, id)
		}
	}
}

// The picker is convenience UI, not a second authority plane. Keep the exact
// retrieval and deferred-call sequence visible in source so a later cosmetic
// rewrite cannot silently turn "Use in chat" into an unbound @name hint.
func TestCapabilityPickerPreservesSessionAndApprovalContracts(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{
		`contract?.skill_catalog`, `binding.skill_id`, `binding.version_id`,
		`not in this session’s frozen catalog`, `tool_search`, `tool_describe`, `tool_call`,
		`do not bypass required approval`, `preserve every approval requirement`,
	} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("capability picker lost its authority marker %q", marker)
		}
	}
	if strings.Contains(javascript, "`${existing}${separator}@${name}") {
		t.Fatal("capability picker regressed to an unbound @name hint")
	}
}

// TestMotionIsTokenisedAndReadableWithoutIt locks two rules that fail silently.
// Durations written as literals are several answers to one question, and an
// animation that carries state on its own is invisible to anyone who turns
// motion off.
func TestMotionIsTokenisedAndReadableWithoutIt(t *testing.T) {
	stylesheet := mustUIFile(t, "ui/style.css")
	for _, token := range []string{"--dur-press:", "--dur-arrive:", "--dur-settle:", "--dur-hold-done:"} {
		if !strings.Contains(stylesheet, token) {
			t.Errorf("motion token %s is missing", token)
		}
	}
	// A literal second inside transition/animation is a second place answering
	// the same question as the token set.
	literal := regexp.MustCompile(`(?:transition|animation)[^;{}]*?\b\d*\.?\d+m?s\b`)
	for _, found := range literal.FindAllString(stylesheet, -1) {
		if !strings.Contains(found, "var(--dur-") {
			t.Errorf("duration written as a literal instead of a token: %q", found)
		}
	}
	if strings.Count(stylesheet, "prefers-reduced-motion") < 3 {
		t.Error("motion is used in more places than it is guarded for reduced-motion")
	}
}

// TestActionsReportOnThemselves keeps feedback on the control that was pressed.
// A toast belongs to something that happened elsewhere.
func TestActionsReportOnThemselves(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{"function runAction", "aria-busy", "data-action-state"} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("action feedback is missing %s", marker)
		}
	}
}
