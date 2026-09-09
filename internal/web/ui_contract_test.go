package web

import (
	"io/fs"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestHermetrixCockpitExposesEveryNativeWorkbenchRoom(t *testing.T) {
	index := mustUIFile(t, "ui/index.html")
	javascript := mustUIFile(t, "ui/runtime.js") + "\n" + mustUIFile(t, "ui/app.js")
	stylesheet := mustUIFile(t, "ui/style.css") + "\n" + mustUIFile(t, "ui/codex-theme.css")
	for _, marker := range []string{
		`id="sessionDock"`, `id="workspacePaneHost"`, `id="paneAdd"`, `id="paneCountLabel"`,
		`id="commandButton"`, `id="commandDialog"`,
		`id="capabilityDialog"`, `<script src="/runtime.js" defer></script>`,
		`<link rel="stylesheet" href="/vendor/ide.css">`, `<script src="/vendor/ide.js" defer></script>`,
	} {
		if !strings.Contains(index, marker) {
			t.Errorf("cockpit HTML is missing %s", marker)
		}
	}
	for _, marker := range []string{
		`/api/projects/`, `/api/terminals`, `/api/browser/tabs`, `/api/deliverables`, `/api/teams`,
		`/api/team-runs`, `renderWorkbenchFiles`, `renderWorkbenchTerminal`, `renderWorkbenchBrowser`,
		`renderWorkbenchArtifacts`, `renderWorkbenchTeam`, `mountWorkbenchTerminal`, `resizeTerminalTo`,
		`HermetrixIDE.createTerminal`, `HermetrixIDE.createEditor`, `codeTabs`, `terminalCursor`,
		`data-editor-action`, `editorCommandFor`, `runEditorAction`, `codeSymbols`, `data-code-symbol`, `codeCursor`,
		`renderDeliverableDraftPreview`, `data-team-member`, `data-team-task`, `/cancel`, `cancelWorkbenchTeamRun`,
		`data-team-approval`, `/approval`, `decideWorkbenchTeamApproval`,
		// The right workspace is a real 1–4 pane canvas. Every pane can choose
		// any native work surface and both internal axes are draggable.
		`const MAX_PANES = 4`, `const PANE_CONTENT`, `id: "review"`, `id: "artifacts"`, `id: "team"`,
		`data-pane-content`, `data-pane-divider`, `startPaneDrag`, `--pane-split-x`, `--pane-split-y`,
		`draggable="true"`, `data-pane-drag`, `data-pane-drop-edge`, `movePane`, `placePaneAtEdge`,
		`bottom-wide`, `top-wide`, `left-wide`, `right-wide`, `paneLayoutSelect`,
		// Navigation is persistent and projects own their session rows.
		`renderRailNavigation`, `data-rail-view`, `data-rail-project`, `rail-project-sessions`,
		`data-rail-project-toggle`, `railProjectsOpen`, `railSetupOpen`, `id="sessionSetup"`,
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
		`.workspace-pane-host`, `.pane-divider`, `.rail-primary`, `.app-shell`, `.config-nav-item`, `.config-pane`, `.command-dialog`,
		`.capability-dialog`, `details.tool-receipt`, `.tool-center-grid`,
		`.rail-project-toggle`, `.rail-projects[open]`, `overflow-y: auto`, `scrollbar-gutter: stable`,
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

// TestUnbuiltViewsSaySoRatherThanShowingNothing keeps the shell honest while
// Work and Knowledge are still specs. Code now has a real file/terminal/browser
// workspace, so its rail must not describe that implemented surface as spec 2.
func TestUnbuiltViewsSaySoRatherThanShowingNothing(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{"function switchView", "const VIEWS", "state.view"} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("view switching is missing %s", marker)
		}
	}
	for _, spec := range []string{"spec 3", "spec 4"} {
		if !strings.Contains(javascript, spec) {
			t.Errorf("an unbuilt view does not name the spec it is waiting on (%s)", spec)
		}
	}
	// The doors were three ways of switching one inspector room, not three ways
	// of working. They are gone.
	if strings.Contains(mustUIFile(t, "ui/index.html"), `data-door=`) {
		t.Error("the old door switch survived the redesign")
	}
}

// TestLayoutIsRememberedPerProjectAndView pins where the layout lives and why.
// It is a preference of this screen, not data about the project, so it must not
// reach SQLite and must not travel in a backup.
func TestLayoutIsRememberedPerProjectAndView(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	for _, marker := range []string{"function saveLayout", "function applyLayout", "hermetrix.layout."} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("layout persistence is missing %s", marker)
		}
	}
	// Reading storage must never be the thing that blanks the screen.
	if !strings.Contains(javascript, "catch") {
		t.Error("layout code does not guard against unreadable storage")
	}
	for _, forbidden := range []string{"/api/layout", "layout_json"} {
		if strings.Contains(javascript, forbidden) {
			t.Errorf("layout is being sent to the server (%s); it is a per-screen preference", forbidden)
		}
	}
}

// TestPanesGiveTerminalAndBrowserRoom is the correction the spec opens with: a
// terminal in a 320px rail is the same mistake as a file tree in one. Content
// that needs room must be able to take it, up to a ceiling that stays testable.
func TestPanesGiveTerminalAndBrowserRoom(t *testing.T) {
	javascript := mustUIFile(t, "ui/app.js")
	stylesheet := mustUIFile(t, "ui/style.css") + "\n" + mustUIFile(t, "ui/codex-theme.css")
	for _, marker := range []string{
		"const PANE_CONTENT", "function splitPane", "function setPaneContent",
		"function maximisePane", "function movePane", "function placePaneAtEdge", "MAX_PANES",
	} {
		if !strings.Contains(javascript, marker) {
			t.Errorf("pane support is missing %s", marker)
		}
	}
	// Terminal and browser are pane content now, not rooms in a fixed strip.
	for _, id := range []string{`id: "terminal"`, `id: "browser"`} {
		if !strings.Contains(javascript, id) {
			t.Errorf("pane content is missing %s", id)
		}
	}
	if !strings.Contains(stylesheet, ".pane-grid") || !strings.Contains(stylesheet, "--pane-columns") ||
		!strings.Contains(stylesheet, ".pane-arrangement-bottom-wide") ||
		!strings.Contains(stylesheet, ".pane-arrangement-top-wide") {
		t.Error("stylesheet does not define the pane grid")
	}
}

// colourLiteralCeiling is the count of hardcoded colour values (hex,
// rgb()/rgba(), and bare CSS colour keywords) that live outside every
// :root block in ui/style.css. A hardcoded colour is a second place that
// answers "what is this colour" -- this ceiling can only go down as
// literals migrate into tokens. 194 was where counting started; Task 4
// migrated the shell/header/rail chrome and brought it to 163; Task 5
// migrated the chat/main reading surfaces (.message/.chat-*/.composer/
// .stats/.panel/.empty and the inline tool receipt and approval cards) and
// brought it to 123. Task 6 migrated the settings and workbench surfaces,
// bringing it to 72, then finished the sweep on hex/rgb literals -- every
// one of those now resolves to a token, which is how the ceiling reached 0.
//
// That 0 was not the whole truth: colourLiteralsIn only ever matched
// #hex and rgb()/rgba(), so every bare CSS colour keyword (`color: white`)
// was invisible to it and never counted. Extending the checker to also see
// bare keywords (colourKeywordsIn, below) re-measured the file honestly and
// found 10 literals it had missed -- all `white`, at .tab:hover,
// .search input, the select/input/textarea reset, .metric strong,
// .chat-welcome h3, .toast, .toast.error's color-mix, dialog,
// .workbench-tab:hover and .browser-shot. Those 10 became var(--text)
// (primary text on a dark surface) everywhere but .toast/.toast.error and
// .browser-shot, which are light surfaces inside the dark app and so took
// --doc-paper/--doc-ink -- the tokens already named for exactly that case.
// The ceiling returns to 0, this time honestly measured against keywords
// too.
const colourLiteralCeiling = 0

func TestColourLiteralsOnlyLiveInTokens(t *testing.T) {
	found := colourLiteralsIn(mustUIFile(t, "ui/style.css"))
	if len(found) > colourLiteralCeiling {
		t.Errorf("ค่าสีที่เขียนตรงนอก :root มี %d ค่า เพดานคือ %d — เพดานนี้ลดได้อย่างเดียว: %v",
			len(found), colourLiteralCeiling, found)
	}
}

// TestColourLiteralCheckerSeesLiteralsAndIgnoresTokens proves the checker
// actually catches hardcoded colours rather than trivially passing by
// returning zero every time.
func TestColourLiteralCheckerSeesLiteralsAndIgnoresTokens(t *testing.T) {
	css := `:root { --bg: #0c0e12; }
.a { color: #ff0000; }
.b { background: rgba(1,2,3,.4); }
.c { color: var(--bg); }`
	found := colourLiteralsIn(css)
	if len(found) != 2 {
		t.Fatalf("อยากได้ 2 ค่า (#ff0000 กับ rgba(...)) ได้ %v", found)
	}
}

// TestColourLiteralCheckerSeesBareKeywordsToo regression-guards the blind
// spot a review caught: colourLiteralsIn matched only #hex and rgb()/rgba(),
// so a rule like ".tab:hover { color: white; }" counted as zero literals.
// This proves the checker now sees a bare keyword used as a real colour
// value, while still ignoring the things that only look like one:
//   - a class name that contains a colour word (.pill.green) -- the word
//     never sits in value position, so a checker naively scanning the whole
//     file with \bgreen\b would over-count here.
//   - a custom-property name that ends in a colour word (--accent-lime),
//     referenced with var(...) -- the reference must be stripped before
//     keyword-hunting, the same way literalDurationsIn strips var(--dur-*)
//     before hunting bare durations, or "lime" inside "--accent-lime"
//     would be reported as if it were a raw keyword.
//   - transparent and currentColor, which are structural values (and
//     transparent is used deliberately inside color-mix()), not named hues.
func TestColourLiteralCheckerSeesBareKeywordsToo(t *testing.T) {
	css := `:root { --bg: #0c0e12; --accent-lime: var(--accent); }
.tab:hover { color: white; }
.pill.green { color: var(--accent); }
.toast { background: color-mix(in srgb, var(--danger) 50%, white); }
.a { background: var(--accent-lime); }
.b { color: transparent; }
.c { color: currentColor; }`
	found := colourLiteralsIn(css)
	if len(found) != 2 {
		t.Fatalf("อยากได้ 2 ค่า (white กับ white ใน color-mix) ได้ %v", found)
	}
	for _, literal := range found {
		if !strings.EqualFold(literal, "white") {
			t.Fatalf("เจอค่าที่ไม่ควรเจอ: %q ใน %v", literal, found)
		}
	}
}

// colourLiteralsIn returns every hardcoded colour value outside all :root
// blocks: #hex, rgb()/rgba(), and bare CSS colour keywords used as an
// actual value (not a class name, not a custom-property identifier, and
// not the structural values transparent/currentColor).
//
// :root is stripped first, always, because that is where colour values
// belong. This file has four :root blocks (main theme, media query, and
// density) -- every one of them has to be stripped, not just the first.
func colourLiteralsIn(css string) []string {
	stripped := rootBlockPattern.ReplaceAllString(css, "")
	found := hexPattern.FindAllString(stripped, -1)
	found = append(found, rgbPattern.FindAllString(stripped, -1)...)
	return append(found, colourKeywordsIn(stripped)...)
}

// colourKeywordsIn finds bare CSS colour keywords used as declaration
// values. It first isolates each "property: value;" declaration -- not the
// whole file -- because a colour word can legitimately appear in a
// selector (.pill.green) or a custom-property name (--accent-lime), and a
// selector or identifier is never where colour keyword-hunting is
// wanted: a keyword only counts when it sits in a value. Then, inside each
// value, it strips every var(...) reference before searching, the same way
// literalDurationsIn strips var(--dur-*) refs before hunting bare
// durations -- otherwise "lime" inside "var(--accent-lime)" reads as a raw
// keyword when it is actually a token reference.
func colourKeywordsIn(css string) []string {
	var found []string
	for _, declaration := range colourDeclarationPattern.FindAllString(css, -1) {
		colon := strings.Index(declaration, ":")
		if colon == -1 {
			continue
		}
		value := colourTokenUse.ReplaceAllString(declaration[colon+1:], "")
		found = append(found, colourKeywordPattern.FindAllString(value, -1)...)
	}
	return found
}

var (
	rootBlockPattern = regexp.MustCompile(`(?s):root(\[[^\]]*\])?\s*\{[^}]*\}`)
	hexPattern       = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	// rgbPattern matches every CSS colour function, not just rgb()/rgba().
	// hsl()/hsla()/hwb()/lab()/lch()/oklab()/oklch()/color() are just as
	// hardcoded a colour as rgb() is -- a value that answers "what is this
	// colour" a second time instead of pointing at a token. The alternation
	// requires the literal "(" immediately after the function name, so
	// "color:" (the property) never matches -- there is no "(" after it --
	// and "color-mix(" never matches either -- "color" is followed by "-",
	// not "(", so color-mix keeps working as this file's deliberate tinting
	// technique rather than being flagged as a literal.
	rgbPattern = regexp.MustCompile(`\b(rgba?|hsla?|hwb|oklab|oklch|lab|lch|color)\(`)
	// colourDeclarationPattern isolates one "property: value" declaration at
	// a time. The trailing ";" used to be required, which silently skipped a
	// final declaration in a block written without one (e.g. ".x{color:red}")
	// -- ";" is now optional, so the match still stops at the value's real
	// end (colourTokenUse and colourKeywordPattern both operate on
	// [^;{}]+, which already can't cross a "}") whether or not a semicolon
	// follows it.
	colourDeclarationPattern = regexp.MustCompile(`[a-zA-Z-]+\s*:\s*[^;{}]+;?`)
	colourTokenUse           = regexp.MustCompile(`var\([^)]*\)`)
	// colourKeywordPattern lists the standard CSS named colours, minus the
	// two structural keywords transparent and currentColor -- those are
	// never a hue: transparent is used deliberately inside color-mix(), and
	// currentColor names "whatever --text/--accent/etc already resolved
	// to" rather than answering "what is this colour" a second time.
	colourKeywordPattern = regexp.MustCompile(`(?i)\b(` + strings.Join(cssColourKeywords, "|") + `)\b`)
	cssColourKeywords     = []string{
		"aliceblue", "antiquewhite", "aqua", "aquamarine", "azure", "beige", "bisque", "black",
		"blanchedalmond", "blue", "blueviolet", "brown", "burlywood", "cadetblue", "chartreuse",
		"chocolate", "coral", "cornflowerblue", "cornsilk", "crimson", "cyan", "darkblue", "darkcyan",
		"darkgoldenrod", "darkgray", "darkgreen", "darkgrey", "darkkhaki", "darkmagenta",
		"darkolivegreen", "darkorange", "darkorchid", "darkred", "darksalmon", "darkseagreen",
		"darkslateblue", "darkslategray", "darkslategrey", "darkturquoise", "darkviolet", "deeppink",
		"deepskyblue", "dimgray", "dimgrey", "dodgerblue", "firebrick", "floralwhite", "forestgreen",
		"fuchsia", "gainsboro", "ghostwhite", "gold", "goldenrod", "gray", "green", "greenyellow",
		"grey", "honeydew", "hotpink", "indianred", "indigo", "ivory", "khaki", "lavender",
		"lavenderblush", "lawngreen", "lemonchiffon", "lightblue", "lightcoral", "lightcyan",
		"lightgoldenrodyellow", "lightgray", "lightgreen", "lightgrey", "lightpink", "lightsalmon",
		"lightseagreen", "lightskyblue", "lightslategray", "lightslategrey", "lightsteelblue",
		"lightyellow", "lime", "limegreen", "linen", "magenta", "maroon", "mediumaquamarine",
		"mediumblue", "mediumorchid", "mediumpurple", "mediumseagreen", "mediumslateblue",
		"mediumspringgreen", "mediumturquoise", "mediumvioletred", "midnightblue", "mintcream",
		"mistyrose", "moccasin", "navajowhite", "navy", "oldlace", "olive", "olivedrab", "orange",
		"orangered", "orchid", "palegoldenrod", "palegreen", "paleturquoise", "palevioletred",
		"papayawhip", "peachpuff", "peru", "pink", "plum", "powderblue", "purple", "rebeccapurple",
		"red", "rosybrown", "royalblue", "saddlebrown", "salmon", "sandybrown", "seagreen", "seashell",
		"sienna", "silver", "skyblue", "slateblue", "slategray", "slategrey", "snow", "springgreen",
		"steelblue", "tan", "teal", "thistle", "tomato", "turquoise", "violet", "wheat", "white",
		"whitesmoke", "yellow", "yellowgreen",
	}
)

// contrast วัดด้วยเลข ไม่ใช่สายตา เพราะ Task ถัดไปเปลี่ยนทุกสีพร้อมกัน
// และ "ดูโอเคนะ" ไม่ใช่หลักฐาน
func TestPaletteMeetsWCAGAA(t *testing.T) {
	css := mustUIFile(t, "ui/style.css")
	backgrounds := []string{"--bg", "--surface", "--surface-2"}
	for _, item := range []struct {
		token string
		min   float64
	}{
		{"--text", 4.5},
		{"--muted", 4.5},
		{"--faint", 3.0},
		{"--accent", 4.5},
		{"--ok", 4.5},
		{"--warn", 4.5},
		{"--danger", 4.5},
	} {
		for _, bg := range backgrounds {
			foreground := tokenValue(css, item.token)
			background := tokenValue(css, bg)
			if foreground == "" {
				t.Fatalf("โทเคน %s ไม่มีใน :root — contrast วัดไม่ได้ ไม่ใช่ผ่าน", item.token)
			}
			if background == "" {
				t.Fatalf("โทเคนพื้น %s ไม่มีใน :root — contrast วัดไม่ได้ ไม่ใช่ผ่าน", bg)
			}
			got := contrastRatio(foreground, background)
			if got < item.min {
				t.Errorf("%s บน %s = %.2f ต้อง ≥ %.1f", item.token, bg, got, item.min)
			}
		}
	}
}

// เช็คเกอร์ต้องรู้จักคู่ที่ตกจริง ไม่ใช่คืนเลขสวยเสมอ
func TestContrastCheckerRejectsALowPair(t *testing.T) {
	if got := contrastRatio("#777777", "#6f6f6f"); got >= 4.5 {
		t.Fatalf("เทาบนเทาได้ %.2f ซึ่งไม่ควรผ่าน 4.5", got)
	}
	if got := contrastRatio("#ffffff", "#000000"); got < 20 {
		t.Fatalf("ขาวบนดำได้ %.2f ควรใกล้ 21", got)
	}
}

// tokenValue อ่านค่าโทเคนจาก :root block แรก ถ้าไม่เจอคืนค่าว่าง
// ซึ่ง contrastRatio จะคืน 0 และ test จะฟ้องว่าโทเคนหาย ไม่ใช่ผ่านเงียบ
func tokenValue(css, name string) string {
	pattern := regexp.MustCompile(regexp.QuoteMeta(name) + `:\s*(#[0-9a-fA-F]{3,8})`)
	match := pattern.FindStringSubmatch(css)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

// contrastRatio คำนวณตาม WCAG 2.x relative luminance
func contrastRatio(foreground, background string) float64 {
	first, second := relativeLuminance(foreground), relativeLuminance(background)
	if first < second {
		first, second = second, first
	}
	return (first + 0.05) / (second + 0.05)
}

func relativeLuminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0
	}
	channel := func(offset int) float64 {
		value, err := strconv.ParseInt(hex[offset:offset+2], 16, 0)
		if err != nil {
			return 0
		}
		scaled := float64(value) / 255
		if scaled <= 0.03928 {
			return scaled / 12.92
		}
		return math.Pow((scaled+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(0) + 0.7152*channel(2) + 0.0722*channel(4)
}

// ชื่อที่ยกเลิกแล้วต้องหายจริง ไม่ใช่ยังเขียนได้เพราะมี alias ค้าง
func TestRetiredTokensStayRetired(t *testing.T) {
	css := mustUIFile(t, "ui/style.css")
	for _, retired := range []string{"--accent-lime", "--accent-violet", "--blue", "--amber", "--red",
		"--panel-2", "--panel", "--panel-raised", "--panel-hover", "--focus"} {
		if strings.Contains(css, retired) {
			t.Errorf("โทเคนที่ยกเลิกแล้วยังอยู่: %s", retired)
		}
	}
}

// scaleValueCeiling is the radius/type-scale sibling of colourLiteralCeiling:
// the count of hardcoded border-radius, font-size and font: px values that
// live outside every :root block in ui/style.css. Colour has had a ceiling
// test since early in this branch; the radius and type-scale migration --
// roughly half the branch's diff -- had none, even though it finished just
// as clean. It measures 0 today. As with the colour ceiling, this number can
// only go down: it is a floor against drift back into literals, not a target
// to hit.
//
// Spacing (padding/gap/margin) is deliberately not covered here: those
// legitimately carry many one-off px values for optical alignment, and a
// zero ceiling there would be false precision, not a real invariant.
const scaleValueCeiling = 0

func TestScaleValuesOnlyLiveInTokens(t *testing.T) {
	found := scaleLiteralsIn(mustUIFile(t, "ui/style.css"))
	if len(found) > scaleValueCeiling {
		t.Errorf("ค่าสเกลที่เขียนตรงนอก :root มี %d ค่า เพดานคือ %d — เพดานนี้ลดได้อย่างเดียว: %v",
			len(found), scaleValueCeiling, found)
	}
}

// TestScaleValueCheckerSeesLiteralsAndIgnoresTokens proves scaleLiteralsIn
// actually catches hardcoded radius/type-scale px values rather than
// trivially returning zero every time, and that it does not flag the
// legitimate exceptions: font-size: 0 (a layout trick for hiding text while
// keeping an element in the accessibility tree/tab order, used twice in the
// file) and border-radius values of 0, 50% or inherit (a square corner, a
// circle, and "match my container" are not answers to "what size is this
// radius" -- they carry no scale value to migrate into a token, so a px
// hunter naturally leaves them alone: none of the four ever has a "px"
// suffix for scalePxPattern to find).
func TestScaleValueCheckerSeesLiteralsAndIgnoresTokens(t *testing.T) {
	css := `:root { --radius: 12px; }
.a { border-radius: 7px; }
.b { font-size: 15px; }
.c { border-radius: var(--radius); }
.d { border-radius: 0; }
.e { border-radius: 50%; }
.f { border-radius: inherit; }
.g { font-size: 0; }
.h { font: var(--text-2xs)/var(--leading-2xs) ui-monospace,monospace; }`
	found := scaleLiteralsIn(css)
	if len(found) != 2 {
		t.Fatalf("อยากได้ 2 ค่า (7px กับ 15px) ได้ %v", found)
	}
	for _, literal := range found {
		if literal != "7px" && literal != "15px" {
			t.Fatalf("เจอค่าที่ไม่ควรเจอ: %q ใน %v", literal, found)
		}
	}
}

// scaleLiteralsIn returns every hardcoded border-radius/font-size/font: px
// value outside all :root blocks, the same way colourLiteralsIn does for
// colour. It isolates each matching declaration, strips var(...) references
// out of its value (so a token reference like var(--radius) is never
// mistaken for a literal), and then looks for a bare px number in what is
// left.
func scaleLiteralsIn(css string) []string {
	stripped := rootBlockPattern.ReplaceAllString(css, "")
	var found []string
	for _, declaration := range scaleDeclarationPattern.FindAllString(stripped, -1) {
		colon := strings.Index(declaration, ":")
		if colon == -1 {
			continue
		}
		value := colourTokenUse.ReplaceAllString(declaration[colon+1:], "")
		found = append(found, scalePxPattern.FindAllString(value, -1)...)
	}
	return found
}

var (
	// scaleDeclarationPattern isolates one border-radius/font-size/font
	// declaration at a time, the same way colourDeclarationPattern isolates
	// a colour declaration -- and for the same reason: scalePxPattern must
	// only ever look inside a value, never a selector, and a bare word
	// boundary before "font" keeps "font-family:" from being read as the
	// "font" shorthand (the family list runs into real font names that are
	// not px values, so it would never false-positive here, but the
	// boundary keeps the isolated text limited to a property this test
	// actually governs).
	scaleDeclarationPattern = regexp.MustCompile(`\b(?:border-radius|font-size|font)\s*:\s*[^;{}]+;?`)
	// scalePxPattern finds a bare px number the way bareDuration finds a
	// bare duration: no assumption that the number has a leading digit, so
	// ".5px" is caught along with "7px".
	scalePxPattern = regexp.MustCompile(`\d*\.?\d+px\b`)
)
