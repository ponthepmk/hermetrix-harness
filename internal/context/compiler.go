package context

import (
	stdcontext "context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrDirectToolsOverflow = errors.New("direct tool schemas exceed profile budget")
	ErrPinnedOverflow      = errors.New("pinned context exceeds profile budget")
	// ErrLedgerImbalance means the compiler cannot say where every token went.
	// It fails the compile rather than shipping a report with a hole in it,
	// because a budget nobody can reconcile is a declared budget, not a
	// certified one.
	ErrLedgerImbalance = errors.New("context token ledger does not balance")
	ErrContextOverflow = errors.New("compiled context exceeds allocated window")
)

type Compiler struct {
	estimator Estimator
	spiller   Spiller
	compactor Compactor
}

// WithEstimator returns a compiler that measures with the given estimator and
// is otherwise identical. The receiver is not modified, so concurrent turns on
// different models never share a calibration.
func (c *Compiler) WithEstimator(estimator Estimator) *Compiler {
	if estimator == nil {
		return c
	}
	clone := *c
	clone.estimator = estimator
	return &clone
}

func NewCompiler(estimator Estimator, spiller Spiller, compactor Compactor) *Compiler {
	return &Compiler{estimator: estimator, spiller: spiller, compactor: compactor}
}

func (c *Compiler) Compile(ctx stdcontext.Context, request Request) (Compiled, error) {
	if err := request.Profile.Validate(); err != nil {
		return Compiled{}, err
	}
	if c.estimator == nil {
		return Compiled{}, fmt.Errorf("context compiler requires a token estimator")
	}
	profile := request.Profile
	report := Report{Profile: profile.Name, TotalContext: profile.Total,
		OutputReserve: profile.OutputReserve, UncertaintyReserve: profile.UncertaintyReserve,
		WorstCaseToolBurst: request.WorstCaseToolBurst,
		Slices: map[string]SliceUsage{
			"system":         {Budget: profile.SystemBudget},
			"tools":          {Budget: profile.DirectToolBudget},
			"skills_project": {Budget: profile.SkillProjectBudget},
			"pinned":         {Budget: profile.PinnedBudget},
			"active":         {Budget: profile.ActiveBudget},
		}}
	toolTokens := 0
	for _, tool := range request.DirectTools {
		toolTokens += c.estimator.Count(tool.BillableText())
	}
	report.Slices["tools"] = SliceUsage{Budget: profile.DirectToolBudget, Used: toolTokens}
	if toolTokens > profile.DirectToolBudget {
		return Compiled{}, fmt.Errorf("%w: used=%d budget=%d", ErrDirectToolsOverflow, toolTokens, profile.DirectToolBudget)
	}
	report.OriginalTokens = tokens(c.estimator, request.Fragments)
	fragments := deduplicate(request.Fragments)
	afterDedup := tokens(c.estimator, fragments)
	report.DeduplicatedTokens = report.OriginalTokens - afterDedup
	report.DeduplicatedFragments = len(request.Fragments) - len(fragments)
	fragments = propagatePairPins(fragments)
	var err error
	fragments, report.Spilled, err = c.spillLargeTools(ctx, fragments, profile.MaxInlineTool)
	if err != nil {
		return Compiled{}, err
	}
	report.SpilledTokens = afterDedup - tokens(c.estimator, fragments)
	groups := map[string][]Fragment{"system": {}, "skills_project": {}, "pinned": {}, "active": {}}
	for _, fragment := range fragments {
		slice := sliceFor(fragment)
		groups[slice] = append(groups[slice], fragment)
	}
	selectedSystem, droppedSystem, systemUsed := c.selectWithin(groups["system"], profile.SystemBudget)
	selectedSkills, droppedSkills, skillsUsed := c.selectWithin(groups["skills_project"], profile.SkillProjectBudget)
	selectedPinned, droppedPinned, pinnedUsed := c.selectWithin(groups["pinned"], profile.PinnedBudget)
	if len(droppedPinned) > 0 {
		return Compiled{}, fmt.Errorf("%w: used=%d budget=%d", ErrPinnedOverflow,
			pinnedUsed+tokens(c.estimator, droppedPinned), profile.PinnedBudget)
	}
	activeBudget := profile.ActiveBudget - request.WorstCaseToolBurst
	if activeBudget < 0 {
		return Compiled{}, fmt.Errorf("%w: worst-case tool burst exceeds active budget", ErrContextOverflow)
	}
	selectedActive, droppedActive, activeUsed := c.selectActive(groups["active"], activeBudget, profile.SummaryTarget)
	var checkpoint Fragment
	if len(droppedActive) > 0 && c.compactor != nil {
		// The focus is the session's current goal, which is what the pinned
		// slice holds. Handing it to the compactor is what lets the checkpoint
		// keep the part of an exchange that bears on the work rather than the
		// part that happens to sit at its ends.
		checkpoint, err = c.compactor.Compact(ctx, CompactRequest{Fragments: droppedActive,
			TargetTokens: profile.SummaryTarget, Estimator: c.estimator, Focus: focusOf(request.Fragments)})
		if err != nil {
			return Compiled{}, fmt.Errorf("compact context: %w", err)
		}
		if checkpoint.Content != "" {
			checkpointTokens := c.estimator.Count(checkpoint.Content)
			if activeUsed+checkpointTokens > activeBudget {
				return Compiled{}, fmt.Errorf("%w: compactor exceeded its target", ErrContextOverflow)
			}
			activeUsed += checkpointTokens
			report.CompactedTokens = checkpointTokens
		}
	}
	report.Slices["system"] = SliceUsage{Budget: profile.SystemBudget, Used: systemUsed}
	report.Slices["skills_project"] = SliceUsage{Budget: profile.SkillProjectBudget, Used: skillsUsed}
	report.Slices["pinned"] = SliceUsage{Budget: profile.PinnedBudget, Used: pinnedUsed}
	report.Slices["active"] = SliceUsage{Budget: profile.ActiveBudget, Used: activeUsed + request.WorstCaseToolBurst}
	selected := append([]Fragment{}, selectedSystem...)
	selected = append(selected, selectedSkills...)
	selected = append(selected, selectedPinned...)
	if checkpoint.Content != "" {
		selected = append(selected, checkpoint)
	}
	selected = append(selected, selectedActive...)
	selected = canonicalOrder(selected)
	dropped := append([]Fragment{}, droppedSystem...)
	dropped = append(dropped, droppedSkills...)
	dropped = append(dropped, droppedActive...)
	for _, fragment := range selected {
		report.SelectedIDs = append(report.SelectedIDs, fragment.ID)
		report.SelectedTokens += c.estimator.Count(fragment.Content)
	}
	for _, fragment := range dropped {
		report.DroppedIDs = append(report.DroppedIDs, fragment.ID)
		report.DroppedTokens += c.estimator.Count(fragment.Content)
	}
	// The template wraps every message the selection produces. System-kind
	// fragments are joined into one message; everything else becomes its own,
	// except tool calls from the same step which the caller groups. Counting
	// each of those separately over-charges by a wrapper per group, which
	// reserves slightly more than needed rather than slightly less.
	report.TransportTokens = transportCost(request, selected)
	report.PredictedPrompt = report.SelectedTokens + toolTokens + report.TransportTokens
	report.PredictedInput = report.PredictedPrompt + request.WorstCaseToolBurst
	report.Free = profile.Total - profile.OutputReserve - profile.UncertaintyReserve - report.PredictedInput
	if report.PredictedInput+profile.OutputReserve+profile.UncertaintyReserve > profile.Total {
		return Compiled{}, fmt.Errorf("%w: predicted=%d output=%d uncertainty=%d total=%d", ErrContextOverflow,
			report.PredictedInput, profile.OutputReserve, profile.UncertaintyReserve, profile.Total)
	}
	if report.OriginalTokens > 0 {
		report.CompressionRatio = float64(report.SelectedTokens) / float64(report.OriginalTokens)
	}
	if err := report.reconcile(); err != nil {
		return Compiled{}, err
	}
	report.Integrity, err = evaluateIntegrity(fragments, selected, checkpoint.Content)
	if err != nil {
		return Compiled{}, err
	}
	if len(droppedSystem) > 0 {
		report.Warnings = append(report.Warnings, "low-priority system fragments did not fit the stable system slice")
	}
	if len(droppedSkills) > 0 {
		report.Warnings = append(report.Warnings, "some project or selected-skill fragments were omitted by slice budget")
	}
	return Compiled{Fragments: selected, DirectTools: append([]ToolSpec(nil), request.DirectTools...), Report: report}, nil
}

func (c *Compiler) spillLargeTools(ctx stdcontext.Context, fragments []Fragment, limit int) ([]Fragment, []SpillReceipt, error) {
	out := append([]Fragment(nil), fragments...)
	var receipts []SpillReceipt
	for i := range out {
		fragment := &out[i]
		if fragment.Kind != KindToolResult || c.estimator.Count(fragment.Content) <= limit {
			continue
		}
		if c.spiller == nil {
			return nil, nil, fmt.Errorf("tool result %s exceeds inline budget and no artifact spiller is configured", fragment.ID)
		}
		receipt, err := c.spiller.Spill(ctx, "text/plain", []byte(fragment.Content))
		if err != nil {
			return nil, nil, fmt.Errorf("spill tool result %s: %w", fragment.ID, err)
		}
		preview := headTail(compactWhitespace(fragment.Content), 900)
		fragment.Content = fmt.Sprintf("[artifact ref=%s bytes=%d sha256=%s]\nPreview: %s", receipt.Ref, receipt.Bytes, receipt.Checksum, preview)
		fragment.Kind = KindArtifactReceipt
		if fragment.Metadata == nil {
			fragment.Metadata = map[string]string{}
		}
		fragment.Metadata["artifact_ref"] = receipt.Ref
		receipts = append(receipts, receipt)
	}
	return out, receipts, nil
}

func (c *Compiler) selectWithin(fragments []Fragment, budget int) ([]Fragment, []Fragment, int) {
	units := makeUnits(fragments)
	sort.SliceStable(units, func(i, j int) bool {
		if units[i].priority != units[j].priority {
			return units[i].priority > units[j].priority
		}
		return units[i].created.After(units[j].created)
	})
	used := 0
	var selected, dropped []Fragment
	for _, unit := range units {
		cost := tokens(c.estimator, unit.fragments)
		if used+cost <= budget {
			selected = append(selected, unit.fragments...)
			used += cost
		} else {
			dropped = append(dropped, unit.fragments...)
		}
	}
	return canonicalOrder(selected), canonicalOrder(dropped), used
}

func (c *Compiler) selectActive(fragments []Fragment, budget, summaryTarget int) ([]Fragment, []Fragment, int) {
	selectionBudget := budget
	if tokens(c.estimator, fragments) > budget {
		selectionBudget = budget - summaryTarget
		if selectionBudget < 0 {
			selectionBudget = 0
		}
	}
	return c.selectWithin(fragments, selectionBudget)
}

func sliceFor(fragment Fragment) string {
	if fragment.Pinned || fragment.Kind == KindUserGoal || fragment.Kind == KindAcceptanceCriteria {
		return "pinned"
	}
	switch fragment.Kind {
	case KindIdentity, KindPolicy:
		return "system"
	case KindProjectInstruction, KindSelectedSkill:
		return "skills_project"
	default:
		return "active"
	}
}

type fragmentUnit struct {
	fragments []Fragment
	priority  int
	created   time.Time
}

func makeUnits(fragments []Fragment) []fragmentUnit {
	byKey := map[string][]Fragment{}
	var order []string
	for _, fragment := range fragments {
		key := "id:" + fragment.ID
		if fragment.PairID != "" {
			key = "pair:" + fragment.PairID
		}
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], fragment)
	}
	units := make([]fragmentUnit, 0, len(order))
	for _, key := range order {
		fragments := byKey[key]
		unit := fragmentUnit{fragments: fragments}
		for _, fragment := range fragments {
			if fragment.Priority > unit.priority {
				unit.priority = fragment.Priority
			}
			if fragment.CreatedAt.After(unit.created) {
				unit.created = fragment.CreatedAt
			}
		}
		units = append(units, unit)
	}
	return units
}

func deduplicate(fragments []Fragment) []Fragment {
	type item struct {
		fragment Fragment
		index    int
	}
	seen := map[string]item{}
	for index, fragment := range fragments {
		identityPart := ""
		if fragment.Kind == KindToolCall || fragment.Kind == KindToolResult || fragment.Kind == KindArtifactReceipt {
			identityPart = fragment.ID + "|" + fragment.PairID
		}
		sum := sha256.Sum256([]byte(string(fragment.Kind) + "|" + fragment.Version + "|" + identityPart + "|" + fragment.Content))
		key := hex.EncodeToString(sum[:])
		old, ok := seen[key]
		if !ok || fragment.Priority > old.fragment.Priority || fragment.CreatedAt.After(old.fragment.CreatedAt) {
			seen[key] = item{fragment: fragment, index: index}
		}
	}
	items := make([]item, 0, len(seen))
	for _, value := range seen {
		items = append(items, value)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].index < items[j].index })
	out := make([]Fragment, 0, len(items))
	for _, value := range items {
		out = append(out, value.fragment)
	}
	return out
}

func propagatePairPins(fragments []Fragment) []Fragment {
	out := append([]Fragment(nil), fragments...)
	pinnedPairs := map[string]bool{}
	for _, fragment := range out {
		if fragment.PairID != "" && fragment.Pinned {
			pinnedPairs[fragment.PairID] = true
		}
	}
	for index := range out {
		if pinnedPairs[out[index].PairID] {
			out[index].Pinned = true
		}
	}
	return out
}

func evaluateIntegrity(source, selected []Fragment, checkpoint string) (IntegrityReport, error) {
	report := IntegrityReport{}
	selectedIDs := map[string]bool{}
	for _, fragment := range selected {
		selectedIDs[fragment.ID] = true
	}
	pairs := map[string][]Fragment{}
	for _, fragment := range source {
		if fragment.Pinned || fragment.Kind == KindUserGoal || fragment.Kind == KindAcceptanceCriteria {
			report.PinnedTotal++
			if selectedIDs[fragment.ID] {
				report.PinnedRetained++
			}
		}
		if fragment.PairID != "" {
			pairs[fragment.PairID] = append(pairs[fragment.PairID], fragment)
		}
	}
	if report.PinnedTotal == 0 {
		report.EssentialRetention = 1
	} else {
		report.EssentialRetention = float64(report.PinnedRetained) / float64(report.PinnedTotal)
	}
	for _, pair := range pairs {
		report.CausalPairsTotal++
		selectedCount, compactedCount := 0, 0
		for _, fragment := range pair {
			if selectedIDs[fragment.ID] {
				selectedCount++
			}
			marker := fmt.Sprintf("[%s:%s]", fragment.Kind, fragment.ID)
			if strings.Contains(checkpoint, marker) {
				compactedCount++
			}
		}
		switch {
		case selectedCount == len(pair):
			report.CausalPairsSelected++
		case compactedCount == len(pair):
			report.CausalPairsCompacted++
		case selectedCount == 0 && compactedCount == 0:
			report.CausalPairsOmitted++
		default:
			return IntegrityReport{}, fmt.Errorf("%w: causal pair %s was split", ErrContextOverflow, pair[0].PairID)
		}
	}
	if report.PinnedRetained != report.PinnedTotal {
		return IntegrityReport{}, ErrPinnedOverflow
	}
	return report, nil
}

func canonicalOrder(fragments []Fragment) []Fragment {
	out := append([]Fragment(nil), fragments...)
	sort.SliceStable(out, func(i, j int) bool {
		left, right := orderFor(out[i]), orderFor(out[j])
		if left != right {
			return left < right
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func orderFor(fragment Fragment) int {
	switch sliceFor(fragment) {
	case "system":
		return 10
	case "skills_project":
		return 20
	case "pinned":
		return 30
	default:
		if fragment.Kind == KindCheckpoint {
			return 40
		}
		return 50
	}
}

func tokens(estimator Estimator, fragments []Fragment) int {
	total := 0
	for _, fragment := range fragments {
		total += estimator.Count(fragment.Content)
	}
	return total
}

// reconcile balances the token ledger and refuses to return a report that
// cannot say where its input went. Every token that entered leaves exactly one
// way: deduplicated, spilled to an artifact, selected, or dropped. The
// checkpoint is derived text rather than input, so it is subtracted from the
// selected total before the sum is compared.
func (r *Report) reconcile() error {
	r.UnaccountedTokens = r.OriginalTokens - r.DeduplicatedTokens - r.SpilledTokens -
		(r.SelectedTokens - r.CompactedTokens) - r.DroppedTokens
	if r.UnaccountedTokens != 0 {
		return fmt.Errorf(
			"%w: original=%d deduplicated=%d spilled=%d selected=%d compacted=%d dropped=%d unaccounted=%d",
			ErrLedgerImbalance, r.OriginalTokens, r.DeduplicatedTokens, r.SpilledTokens,
			r.SelectedTokens, r.CompactedTokens, r.DroppedTokens, r.UnaccountedTokens)
	}
	// A balanced total is not the same as an honest one. If counting drifts
	// mid-compile the sum still closes, because the difference lands on whichever
	// term has no independent witness. These two do have one.
	if r.DeduplicatedFragments == 0 && r.DeduplicatedTokens != 0 {
		return fmt.Errorf("%w: %d tokens attributed to deduplication but no fragment was removed",
			ErrLedgerImbalance, r.DeduplicatedTokens)
	}
	if len(r.Spilled) == 0 && r.SpilledTokens != 0 {
		return fmt.Errorf("%w: %d tokens attributed to spill but nothing was spilled",
			ErrLedgerImbalance, r.SpilledTokens)
	}
	return nil
}

// transportCost prices the chat template for a selection. Unmeasured providers
// are charged nothing: a guessed overhead is subtracted from usable context on
// every request, and being wrong there is worse than being incomplete.
// focusOf reads the session's current goal out of the fragments. It uses the
// user goal rather than the whole pinned slice: policy and identity are
// constant across every turn, so including them would dilute the focus to the
// point where everything scores the same.
func focusOf(fragments []Fragment) string {
	for _, fragment := range fragments {
		if fragment.Kind == KindUserGoal {
			return fragment.Content
		}
	}
	return ""
}

func transportCost(request Request, selected []Fragment) int {
	// An unmeasured provider carries zeros and is therefore charged nothing by
	// the arithmetic below. That is deliberate: a guessed overhead is subtracted
	// from usable context on every request, and being wrong there is worse than
	// being incomplete.
	messages, systemSeen := 0, false
	for _, fragment := range selected {
		if sliceFor(fragment) == "system" || fragment.Kind == KindProjectInstruction || fragment.Kind == KindSelectedSkill {
			systemSeen = true
			continue
		}
		messages++
	}
	if systemSeen {
		messages++
	}
	return request.RequestOverhead + request.MessageOverhead*messages
}
