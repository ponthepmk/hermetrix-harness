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
		`id="sessionDock"`, `id="workbenchContent"`, `data-workbench="review"`, `data-workbench="files"`,
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
	// Pane widths are density tokens, not literals: the same three-zone layout
	// has to fit a 13" laptop and a desktop display without a second stylesheet.
	for _, marker := range []string{
		`.zones {`, `--rail-width:`, `[data-density="compact"]`,
		`.workbench-tabs`, `.app-shell`, `.config-nav-item`, `.config-pane`, `.command-dialog`,
		`.capability-dialog`, `details.tool-receipt`, `.tool-center-grid`,
	} {
		if !strings.Contains(stylesheet, marker) {
			t.Fatalf("cockpit stylesheet no longer defines the three-zone shell layout: missing %s", marker)
		}
	}
	if strings.Contains(index, "Aetox") || strings.Contains(index, "../Aetox") || strings.Contains(stylesheet, "../Aetox") {
		t.Fatal("product UI copied or exposed Aetox branding instead of Hermetrix identity")
	}
	if strings.Contains(index, `style=`) {
		t.Fatal("UI reintroduced inline style mutations that violate its own CSP")
	}
	// The server sends style-src 'self', so an inline style on a rendered
	// element -- .style.height, .style.display, anything but the one
	// exception below -- is silently dropped rather than merely ugly. The
	// exception is setProperty on a custom property: it writes a variable the
	// stylesheet reads, not a style declaration on an element, and cannot be
	// blocked the same way. That exception is narrow on purpose: it must name
	// a literal custom property (the "--" prefix) at the call site, because
	// setProperty("width", ...) is the exact same forbidden mutation wearing a
	// different method name, and a variable in that slot cannot be checked by
	// inspection at all.
	for _, match := range styleMemberAccess.FindAllStringSubmatch(javascript, -1) {
		if match[1] != "setProperty" {
			t.Fatalf("UI reintroduced an inline style mutation via .style.%s", match[1])
		}
	}
	setPropertyCalls := styleSetPropertyCall.FindAllStringIndex(javascript, -1)
	literalCustomPropertyCalls := styleSetPropertyLiteral.FindAllStringSubmatch(javascript, -1)
	if len(setPropertyCalls) != len(literalCustomPropertyCalls) {
		t.Fatal("style.setProperty is called without a literal custom-property name as its first argument")
	}
	for _, match := range literalCustomPropertyCalls {
		if !strings.HasPrefix(match[1], "--") {
			t.Fatalf("style.setProperty(%q, ...) does not name a custom property", match[1])
		}
	}
}

// styleMemberAccess matches every `.style.<member>` access in app.js.
// styleSetPropertyCall matches a bare setProperty call so it can be counted
// against styleSetPropertyLiteral, which additionally requires that call's
// first argument to be a quoted literal -- a variable there (as in
// `.style.setProperty(token, value)`) cannot be verified by inspection, so it
// is treated the same as a non-custom property rather than trusted.
var (
	styleMemberAccess       = regexp.MustCompile(`\.style\.([A-Za-z_$][\w$]*)`)
	styleSetPropertyCall    = regexp.MustCompile(`\.style\.setProperty\(`)
	styleSetPropertyLiteral = regexp.MustCompile(`\.style\.setProperty\(\s*["']([^"']*)["']`)
)

func mustUIFile(t *testing.T, name string) string {
	t.Helper()
	data, err := fs.ReadFile(uiFiles, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestShellHasOneViewSwitchAndDraggableZones pins the shape the redesign exists
// for. Two switchers is the mistake the mockup made; a zone that cannot be
// resized is the mistake the first draft made when it put a terminal in 320px.
func TestShellHasOneViewSwitchAndDraggableZones(t *testing.T) {
	index := mustUIFile(t, "ui/index.html")
	javascript := mustUIFile(t, "ui/app.js")
	stylesheet := mustUIFile(t, "ui/style.css")
	for _, marker := range []string{
		`id="appHeader"`, `id="projectChip"`, `id="viewSwitch"`,
		`id="zoneRail"`, `id="zoneMain"`, `id="zoneSide"`,
		`data-handle="rail"`, `data-handle="side"`,
	} {
		if !strings.Contains(index, marker) {
			t.Errorf("shell HTML is missing %s", marker)
		}
	}
	if strings.Count(index, `id="viewSwitch"`) != 1 {
		t.Error("there must be exactly one view switch")
	}
	for _, marker := range []string{"setZoneWidth", "data-view", "startZoneDrag"} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("shell JavaScript is missing %s", marker)
		}
	}
	if !strings.Contains(stylesheet, ".app-shell") || !strings.Contains(stylesheet, ".zone-handle") {
		t.Error("stylesheet does not define the resizable three-zone shell")
	}
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

// literalDurationsIn finds every duration written directly into a
// transition or animation declaration, ignoring any duration already
// expressed as a --dur-* token. It inspects one declaration (the text up to
// its terminating semicolon) at a time and strips known token references out
// of it before looking for a bare number-plus-unit, rather than scanning a
// whole declaration with one lazy match. A lazy match stops at the first
// duration-shaped substring it reaches, so a value list such as
// "border-color var(--dur-press), transform .15s" is reported as a single
// match that already contains "var(--dur-" and the literal sitting right
// after it is never inspected at all.
var (
	durationDeclaration = regexp.MustCompile(`(?:transition|animation)\s*:[^;{}]*;`)
	durationTokenUse    = regexp.MustCompile(`var\(--dur-[a-z-]+\)`)
	// No leading \b: a value like ".15s" starts with a non-word rune, so a
	// word boundary never sits between the preceding space and the dot, and
	// requiring one there silently dropped the leading dot from every match.
	bareDuration = regexp.MustCompile(`\d*\.?\d+m?s\b`)
)

func literalDurationsIn(css string) []string {
	var found []string
	for _, declaration := range durationDeclaration.FindAllString(css, -1) {
		withoutTokens := durationTokenUse.ReplaceAllString(declaration, "")
		found = append(found, bareDuration.FindAllString(withoutTokens, -1)...)
	}
	return found
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
	for _, found := range literalDurationsIn(stylesheet) {
		t.Errorf("duration written as a literal instead of a token: %q", found)
	}
	if strings.Count(stylesheet, "prefers-reduced-motion") < 3 {
		t.Error("motion is used in more places than it is guarded for reduced-motion")
	}
}

// TestLiteralDurationCheckCatchesSecondValueInAList regression-guards a
// defect the review caught in the check above: a lazy whole-declaration
// regex reported no literal for a rule shaped like
// "transition: border-color var(--dur-press), transform .15s;" because the
// text it matched already contained a --dur- token, even though the second
// value was still a bare literal. This proves literalDurationsIn inspects
// each value rather than stopping at the first token it sees.
func TestLiteralDurationCheckCatchesSecondValueInAList(t *testing.T) {
	css := `.example { transition: border-color var(--dur-press), transform .15s; }`
	found := literalDurationsIn(css)
	if len(found) != 1 || found[0] != ".15s" {
		t.Fatalf("expected the literal .15s in the second transition value to be caught, got %v", found)
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

// TestPickerIsTheFirstScreen covers the decision that a project is the root of
// everything: the app opens on the choice, and the picker never draws a group
// or a count that has nothing behind it.
func TestPickerIsTheFirstScreen(t *testing.T) {
	index := mustUIFile(t, "ui/index.html")
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{`id="projectPicker"`, `id="pickerSearch"`, `id="pickerPinned"`, `id="pickerRecent"`} {
		if !strings.Contains(index, marker) {
			t.Errorf("picker HTML is missing %s", marker)
		}
	}
	for _, marker := range []string{"function renderPicker", "/open", "session_count", "currentProject"} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("picker JavaScript is missing %s", marker)
		}
	}
	// A count for a subsystem that does not exist is a claim about data that
	// has no store behind it.
	for _, absent := range []string{"task_count", "note_count"} {
		if strings.Contains(javascript, absent) {
			t.Errorf("picker renders %s although no such system exists yet", absent)
		}
	}
}
