package fidelity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"
)

const (
	compilerRevision = "hermetrix-context-compiler-v2"
	verifierRevision = "deterministic-fidelity-verifier-v1"
)

type Service struct {
	store    *store.Store
	compiler *ctxcompiler.Compiler
}

func NewService(dataStore *store.Store, compiler *ctxcompiler.Compiler) *Service {
	return &Service{store: dataStore, compiler: compiler}
}

func (s *Service) SaveCase(ctx context.Context, input CaseInput) (Case, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Language = strings.TrimSpace(input.Language)
	input.BenchmarkClass = strings.TrimSpace(input.BenchmarkClass)
	if input.Name == "" || input.Language == "" || input.BenchmarkClass == "" {
		return Case{}, fmt.Errorf("name, language, and benchmark_class are required")
	}
	if utf8.RuneCountInString(input.Name) > 120 || len(input.Fragments) == 0 || len(input.Fragments) > 5000 {
		return Case{}, fmt.Errorf("invalid corpus case size")
	}
	if input.Expectations.MaxTaskDelta == 0 {
		input.Expectations.MaxTaskDelta = 0.05
	}
	if input.Expectations.MaxPatchDelta == 0 {
		input.Expectations.MaxPatchDelta = 0.05
	}
	if err := validateCase(input); err != nil {
		return Case{}, err
	}
	fragmentsJSON, _ := json.Marshal(input.Fragments)
	expectationsJSON, _ := json.Marshal(input.Expectations)
	if len(fragmentsJSON)+len(expectationsJSON) > 16<<20 {
		return Case{}, fmt.Errorf("corpus case exceeds 16 MiB")
	}
	now := time.Now().UTC()
	if input.ID == "" {
		input.ID = identity.New("ctxcase")
		_, err := s.store.DB.ExecContext(ctx, `INSERT INTO context_eval_cases(id,name,language,benchmark_class,
        fragments_json,expectations_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, input.ID, input.Name,
			input.Language, input.BenchmarkClass, string(fragmentsJSON), string(expectationsJSON), formatTime(now), formatTime(now))
		if err != nil {
			return Case{}, err
		}
	} else {
		result, err := s.store.DB.ExecContext(ctx, `UPDATE context_eval_cases SET name=?,language=?,benchmark_class=?,
        fragments_json=?,expectations_json=?,updated_at=? WHERE id=?`, input.Name, input.Language, input.BenchmarkClass,
			string(fragmentsJSON), string(expectationsJSON), formatTime(now), input.ID)
		if err != nil {
			return Case{}, err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return Case{}, sql.ErrNoRows
		}
	}
	return s.GetCase(ctx, input.ID)
}

func (s *Service) EnsureDefaultCorpus(ctx context.Context) error {
	defaults := []CaseInput{
		{Name: "thai-skill-lifecycle", Language: "th", BenchmarkClass: "instruction-retention",
			Fragments: []ctxcompiler.Fragment{
				{ID: "th-goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "fixture", Trust: "user", Version: "v1", Priority: 100, Pinned: true, Content: "ทุกการแก้ Skill ต้องสร้าง candidate และห้ามแก้ active version โดยตรง"},
				{ID: "th-decision", Kind: ctxcompiler.KindDecision, Scope: "session", Provenance: "fixture", Trust: "verified", Version: "v1", Priority: 90, Content: "ใช้ replay เทียบ baseline กับ candidate ก่อน promotion"},
				{ID: "th-open", Kind: ctxcompiler.KindOpenTask, Scope: "session", Provenance: "fixture", Trust: "verified", Version: "v1", Priority: 80, Content: "ตรวจ stale base และ capability widening"},
			}, Expectations: Expectations{EssentialIDs: []string{"th-goal"}, DecisionIDs: []string{"th-decision"},
				OpenTaskIDs: []string{"th-open"}, TaskAssertions: []string{"candidate", "baseline", "stale base"}, MaxTaskDelta: 0.05}},
		{Name: "english-tool-causality", Language: "en", BenchmarkClass: "tool-causal-recovery",
			Fragments: []ctxcompiler.Fragment{
				{ID: "en-goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "fixture", Trust: "user", Version: "v1", Priority: 100, Pinned: true, Content: "Preserve exact approval and never fabricate tool success."},
				{ID: "en-call", Kind: ctxcompiler.KindToolCall, PairID: "pair-1", Scope: "session", Provenance: "fixture", Trust: "tool", Version: "v1", Priority: 70, Content: "write_file expected_sha256=abc"},
				{ID: "en-result", Kind: ctxcompiler.KindToolResult, PairID: "pair-1", Scope: "session", Provenance: "fixture", Trust: "tool", Version: "v1", Priority: 70, Content: "write rejected because hash changed"},
			}, Expectations: Expectations{EssentialIDs: []string{"en-goal"}, CausalPairIDs: []string{"pair-1"},
				TaskAssertions: []string{"never fabricate tool success", "hash changed"}, MaxTaskDelta: 0.05}},
		pressureCase(),
		approvalCase(),
	}
	for _, item := range defaults {
		var exists int
		if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM context_eval_cases WHERE name=?`, item.Name).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := s.SaveCase(ctx, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) GetCase(ctx context.Context, id string) (Case, error) {
	return scanCase(s.store.DB.QueryRowContext(ctx, `SELECT id,name,language,benchmark_class,fragments_json,
    expectations_json,created_at,updated_at FROM context_eval_cases WHERE id=?`, id))
}

func (s *Service) ListCases(ctx context.Context) ([]Case, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,name,language,benchmark_class,fragments_json,
    expectations_json,created_at,updated_at FROM context_eval_cases ORDER BY language,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Case{}
	for rows.Next() {
		item, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Run(ctx context.Context, caseID, profileName string) (Run, error) {
	item, err := s.GetCase(ctx, caseID)
	if err != nil {
		return Run{}, err
	}
	profile, ok := ctxcompiler.ProfileByName(profileName)
	if !ok {
		return Run{}, fmt.Errorf("unknown context profile %q", profileName)
	}
	started := time.Now().UTC()
	run := Run{ID: identity.New("ctxrun"), CaseID: item.ID, CaseName: item.Name, ProfileName: profile.Name,
		CompilerRevision: compilerRevision, VerifierRevision: verifierRevision, State: "running", StartedAt: started}
	if _, err := s.store.DB.ExecContext(ctx, `INSERT INTO context_eval_runs(id,case_id,profile_name,compiler_revision,
      verifier_revision,state,started_at) VALUES(?,?,?,?,?,?,?)`, run.ID, run.CaseID, run.ProfileName,
		run.CompilerRevision, run.VerifierRevision, run.State, formatTime(started)); err != nil {
		return Run{}, err
	}
	fullJSON, _ := json.Marshal(item.Fragments)
	fullRef, err := s.store.Blobs.Put(fullJSON)
	if err != nil {
		return s.failRun(context.WithoutCancel(ctx), run, err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	compileStarted := time.Now()
	compiled, err := s.compiler.Compile(ctx, ctxcompiler.Request{Profile: profile, Fragments: item.Fragments})
	compileDuration := time.Since(compileStarted)
	runtime.ReadMemStats(&after)
	if err != nil {
		return s.failRun(context.WithoutCancel(ctx), run, err)
	}
	compiledJSON, _ := json.Marshal(compiled)
	compiledRef, err := s.store.Blobs.Put(compiledJSON)
	if err != nil {
		return s.failRun(context.WithoutCancel(ctx), run, err)
	}
	metrics := verify(item, compiled)
	metrics.CompileMilliseconds = compileDuration.Milliseconds()
	if after.HeapAlloc > before.HeapAlloc {
		metrics.PeakHeapDeltaBytes = after.HeapAlloc - before.HeapAlloc
	}
	run.Metrics, run.FullBlobRef, run.CompiledBlobRef = metrics, fullRef, compiledRef
	run.FallbackUsed = metrics.FallbackUsed
	run.State = "completed"
	completed := time.Now().UTC()
	run.CompletedAt = &completed
	metricsJSON, _ := json.Marshal(metrics)
	_, err = s.store.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE context_eval_runs SET state='completed',metrics_json=?,
      full_blob_ref=?,compiled_blob_ref=?,fallback_used=?,completed_at=? WHERE id=?`, string(metricsJSON), fullRef,
		compiledRef, run.FallbackUsed, formatTime(completed), run.ID)
	return run, err
}

func (s *Service) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT r.id,r.case_id,c.name,r.profile_name,r.compiler_revision,
      r.verifier_revision,r.state,r.metrics_json,r.full_blob_ref,r.compiled_blob_ref,r.fallback_used,r.error,
      r.started_at,r.completed_at FROM context_eval_runs r JOIN context_eval_cases c ON c.id=r.case_id
      ORDER BY r.started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Run{}
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) failRun(ctx context.Context, run Run, failure error) (Run, error) {
	completed := time.Now().UTC()
	run.State, run.Error, run.CompletedAt = "failed", failure.Error(), &completed
	_, _ = s.store.DB.ExecContext(ctx, `UPDATE context_eval_runs SET state='failed',error=?,completed_at=? WHERE id=?`,
		run.Error, formatTime(completed), run.ID)
	return run, failure
}

func verify(item Case, compiled ctxcompiler.Compiled) Metrics {
	selected := map[string]ctxcompiler.Fragment{}
	checkpoint := ""
	compiledText := ""
	for _, fragment := range compiled.Fragments {
		selected[fragment.ID] = fragment
		compiledText += "\n" + fragment.Content
		if fragment.Kind == ctxcompiler.KindCheckpoint {
			checkpoint += "\n" + fragment.Content
		}
	}
	source := map[string]ctxcompiler.Fragment{}
	sourceText := ""
	pairs := map[string][]string{}
	for _, fragment := range item.Fragments {
		source[fragment.ID] = fragment
		sourceText += "\n" + fragment.Content
		if fragment.PairID != "" {
			pairs[fragment.PairID] = append(pairs[fragment.PairID], fragment.ID)
		}
	}
	represented := func(id string) bool {
		if _, ok := selected[id]; ok {
			return true
		}
		fragment, ok := source[id]
		return ok && strings.Contains(checkpoint, fmt.Sprintf("[%s:%s]", fragment.Kind, id))
	}
	exact := 0
	for _, id := range item.Expectations.EssentialIDs {
		if value, ok := selected[id]; ok && value.Content == source[id].Content {
			exact++
		}
	}
	metrics := Metrics{EssentialExactRetention: ratio(exact, len(item.Expectations.EssentialIDs)),
		DecisionRecall:  ratioRepresented(item.Expectations.DecisionIDs, represented),
		OpenTaskRecall:  ratioRepresented(item.Expectations.OpenTaskIDs, represented),
		FileStateRecall: ratioRepresented(item.Expectations.FileStateIDs, represented),
		OriginalTokens:  compiled.Report.OriginalTokens, CompiledTokens: compiled.Report.SelectedTokens,
		CompressionRatio: compiled.Report.CompressionRatio}
	metrics.TokensSaved = metrics.OriginalTokens - metrics.CompiledTokens
	for _, pairID := range item.Expectations.CausalPairIDs {
		ids := pairs[pairID]
		count := 0
		for _, id := range ids {
			if represented(id) {
				count++
			}
		}
		if count != 0 && count != len(ids) {
			metrics.CausalPairSplits++
		}
	}
	metrics.TaskSuccessFull = assertionScore(sourceText, item.Expectations.TaskAssertions)
	metrics.TaskSuccessCompiled = assertionScore(compiledText, item.Expectations.TaskAssertions)
	metrics.TaskSuccessDelta = metrics.TaskSuccessFull - metrics.TaskSuccessCompiled
	metrics.PatchCorrectnessFull = assertionScore(sourceText, item.Expectations.PatchAssertions)
	metrics.PatchCorrectnessCompiled = assertionScore(compiledText, item.Expectations.PatchAssertions)
	metrics.PatchCorrectnessDelta = metrics.PatchCorrectnessFull - metrics.PatchCorrectnessCompiled
	for _, claim := range item.Expectations.ForbiddenClaims {
		if strings.Contains(strings.ToLower(compiledText), strings.ToLower(claim)) &&
			!strings.Contains(strings.ToLower(sourceText), strings.ToLower(claim)) {
			metrics.HallucinationCount++
			if strings.Contains(strings.ToLower(claim), "success") || strings.Contains(claim, "สำเร็จ") {
				metrics.FalseSuccessCount++
			}
		}
	}
	accounted := map[string]bool{}
	for _, id := range compiled.Report.SelectedIDs {
		accounted[id] = true
	}
	for _, id := range compiled.Report.DroppedIDs {
		accounted[id] = true
	}
	for _, fragment := range item.Fragments {
		if !accounted[fragment.ID] && !hasEquivalentSelected(fragment, selected) {
			metrics.SilentTruncations++
		}
	}
	for _, fragment := range compiled.Fragments {
		if fragment.Kind == ctxcompiler.KindCheckpoint && strings.Contains(fragment.Provenance, "verified-fallback") {
			metrics.FallbackUsed = true
		}
	}
	// The Phase 9 gate reads "retention of goal/constraint/decision = 100%".
	// Three of those four recalls used to be computed, stored, rendered -- and
	// left out of the verdict. A run that dropped 97% of the decisions a case
	// had declared essential reported Passed=true. The metric existed; the gate
	// did not. Decisions, open tasks and file state are not pinned, so unlike
	// EssentialExactRetention these can genuinely land between 0 and 1, which
	// is exactly why the verdict has to read them.
	metrics.Passed = metrics.EssentialExactRetention == 1 && metrics.DecisionRecall == 1 &&
		metrics.OpenTaskRecall == 1 && metrics.FileStateRecall == 1 && metrics.CausalPairSplits == 0 &&
		metrics.TaskSuccessDelta <= item.Expectations.MaxTaskDelta &&
		metrics.PatchCorrectnessDelta <= item.Expectations.MaxPatchDelta && metrics.HallucinationCount == 0 &&
		metrics.FalseSuccessCount == 0 && metrics.SilentTruncations == 0
	return metrics
}

func validateCase(input CaseInput) error {
	ids, pairs := map[string]bool{}, map[string]bool{}
	text := ""
	for _, fragment := range input.Fragments {
		if strings.TrimSpace(fragment.ID) == "" || ids[fragment.ID] {
			return fmt.Errorf("fragment IDs must be non-empty and unique")
		}
		ids[fragment.ID] = true
		if fragment.PairID != "" {
			pairs[fragment.PairID] = true
		}
		text += "\n" + strings.ToLower(fragment.Content)
	}
	for _, id := range append(append(append([]string{}, input.Expectations.EssentialIDs...), input.Expectations.DecisionIDs...), append(input.Expectations.OpenTaskIDs, input.Expectations.FileStateIDs...)...) {
		if !ids[id] {
			return fmt.Errorf("expectation references unknown fragment %q", id)
		}
	}
	for _, id := range input.Expectations.CausalPairIDs {
		if !pairs[id] {
			return fmt.Errorf("expectation references unknown causal pair %q", id)
		}
	}
	for _, assertion := range append(append([]string{}, input.Expectations.TaskAssertions...), input.Expectations.PatchAssertions...) {
		if !strings.Contains(text, strings.ToLower(assertion)) {
			return fmt.Errorf("assertion %q has no source evidence", assertion)
		}
	}
	if input.Expectations.MaxTaskDelta < 0 || input.Expectations.MaxTaskDelta > 1 ||
		input.Expectations.MaxPatchDelta < 0 || input.Expectations.MaxPatchDelta > 1 {
		return fmt.Errorf("metric tolerances must be between 0 and 1")
	}
	return nil
}

func ratio(count, total int) float64 {
	if total == 0 {
		return 1
	}
	return float64(count) / float64(total)
}

func ratioRepresented(ids []string, represented func(string) bool) float64 {
	count := 0
	for _, id := range ids {
		if represented(id) {
			count++
		}
	}
	return ratio(count, len(ids))
}

func assertionScore(text string, assertions []string) float64 {
	count := 0
	for _, assertion := range assertions {
		if strings.Contains(strings.ToLower(text), strings.ToLower(assertion)) {
			count++
		}
	}
	return ratio(count, len(assertions))
}

func hasEquivalentSelected(fragment ctxcompiler.Fragment, selected map[string]ctxcompiler.Fragment) bool {
	for _, item := range selected {
		if item.Kind == fragment.Kind && item.Version == fragment.Version && item.Content == fragment.Content {
			return true
		}
	}
	return false
}

type caseScanner interface{ Scan(...any) error }

func scanCase(row caseScanner) (Case, error) {
	var item Case
	var fragmentsJSON, expectationsJSON, created, updated string
	if err := row.Scan(&item.ID, &item.Name, &item.Language, &item.BenchmarkClass, &fragmentsJSON,
		&expectationsJSON, &created, &updated); err != nil {
		return Case{}, err
	}
	if err := json.Unmarshal([]byte(fragmentsJSON), &item.Fragments); err != nil {
		return Case{}, err
	}
	if err := json.Unmarshal([]byte(expectationsJSON), &item.Expectations); err != nil {
		return Case{}, err
	}
	item.CreatedAt, _ = parseTime(created)
	item.UpdatedAt, _ = parseTime(updated)
	return item, nil
}

type runScanner interface{ Scan(...any) error }

func scanRun(row runScanner) (Run, error) {
	var item Run
	var metricsJSON, started string
	var completed sql.NullString
	if err := row.Scan(&item.ID, &item.CaseID, &item.CaseName, &item.ProfileName, &item.CompilerRevision,
		&item.VerifierRevision, &item.State, &metricsJSON, &item.FullBlobRef, &item.CompiledBlobRef,
		&item.FallbackUsed, &item.Error, &started, &completed); err != nil {
		return Run{}, err
	}
	_ = json.Unmarshal([]byte(metricsJSON), &item.Metrics)
	item.StartedAt, _ = parseTime(started)
	if completed.Valid {
		value, _ := parseTime(completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

// pressureCase exists because the other two cannot fail.
//
// Both original cases are three fragments and about thirty tokens. They fit
// whole into the smallest profile, so nothing is ever dropped, spilled or
// compacted: every retention metric is 1 by construction and compression_ratio
// is exactly 1. A corpus that asks whether compaction preserves what matters,
// while never causing compaction, answers a question it never posed.
//
// This case puts the compiler under the pressure it is built for. The same four
// fragments that must survive are buried in filler far past the active budget
// of every profile up to extended-128k, so retention is measured against real
// selection rather than against having had room for everything.
//
// Measured on compact-32k when this was written: 255,824 tokens in, 13,905 out,
// with the pinned goal preserved exactly, the decision retained and the causal
// pair unsplit.
func pressureCase() CaseInput {
	fragments := []ctxcompiler.Fragment{
		{ID: "pressure-goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "fixture",
			Trust: "user", Version: "v1", Priority: 100, Pinned: true,
			Content: "ห้ามแก้ active skill โดยตรง ต้องผ่าน candidate เสมอ"},
		{ID: "pressure-decision", Kind: ctxcompiler.KindDecision, Scope: "session", Provenance: "fixture",
			Trust: "verified", Version: "v1", Priority: 90,
			Content: "ใช้ replay เทียบ baseline กับ candidate ก่อน promotion"},
		{ID: "pressure-call", Kind: ctxcompiler.KindToolCall, PairID: "pressure-pair", Scope: "session",
			Provenance: "fixture", Trust: "tool", Version: "v1", Priority: 70,
			Content: "write_file expected_sha256=abc"},
		{ID: "pressure-result", Kind: ctxcompiler.KindToolResult, PairID: "pressure-pair", Scope: "session",
			Provenance: "fixture", Trust: "tool", Version: "v1", Priority: 70,
			Content: "write rejected because hash changed"},
	}
	// Deterministic filler. Each fragment carries its own index so nothing is
	// removed as a duplicate -- deduplication would quietly relieve the pressure
	// this case exists to apply.
	for index := 0; index < pressureFillerFragments; index++ {
		fragments = append(fragments, ctxcompiler.Fragment{
			ID: fmt.Sprintf("pressure-filler-%02d", index), Kind: ctxcompiler.KindConversation,
			Scope: "session", Provenance: "fixture", Trust: "assistant", Version: "v1", Priority: 30,
			Content: fmt.Sprintf("ลำดับ %d ", index) +
				strings.Repeat("บันทึกการสนทนาที่ยาวพอจะกินงบ active slice ทั้งหมด ", pressureFillerRepeats)})
	}
	return CaseInput{Name: "context-pressure", Language: "th", BenchmarkClass: "instruction-retention",
		Fragments: fragments,
		Expectations: Expectations{EssentialIDs: []string{"pressure-goal"},
			DecisionIDs: []string{"pressure-decision"}, CausalPairIDs: []string{"pressure-pair"},
			TaskAssertions: []string{"candidate", "hash changed"}, MaxTaskDelta: 0.05}}
}

// approvalCase mirrors what the system now actually emits.
//
// Until O-40 the corpus was the only place in Hermetrix where a decision or an
// open task existed: the compiler consumed both kinds, the compactor reserved
// them a larger extract, and no producer outside these fixtures ever made one.
// A census of 772 compiled snapshots found decision, open_task and
// acceptance_criteria at max=0.
//
// Approvals closed two thirds of that. This case carries their real shape --
// a human decision that must outlive the tool output it authorised, and a
// request still waiting on a human -- under enough pressure that neither is
// retained by having had room for everything.
func approvalCase() CaseInput {
	fragments := []ctxcompiler.Fragment{
		{ID: "approval-goal", Kind: ctxcompiler.KindUserGoal, Scope: "session", Provenance: "fixture",
			Trust: "user", Version: "v1", Priority: 100, Pinned: true,
			Content: "แก้ order_total ให้คงเป็นจำนวนเต็มสตางค์"},
		{ID: "approval-decision", Kind: ctxcompiler.KindDecision, Scope: "session", Provenance: "approval",
			Trust: "user", Version: "v1", Priority: 90,
			Content: "rodmay approved workspace.write_file (stated reason: corpus drive)"},
		{ID: "approval-open", Kind: ctxcompiler.KindOpenTask, Scope: "session", Provenance: "approval",
			Trust: "system", Version: "v1", Priority: 86,
			Content: "waiting on a human decision: workspace.delete_file (delete) -- remove app/legacy.py"},
		{ID: "approval-call", Kind: ctxcompiler.KindToolCall, PairID: "approval-pair", Scope: "session",
			Provenance: "fixture", Trust: "tool", Version: "v1", Priority: 82,
			Content: "workspace.write_file app/orders.py"},
		{ID: "approval-result", Kind: ctxcompiler.KindToolResult, PairID: "approval-pair", Scope: "session",
			Provenance: "fixture", Trust: "tool", Version: "v1", Priority: 82,
			Content: "wrote app/orders.py (467 bytes)"},
	}
	for index := 0; index < pressureFillerFragments; index++ {
		fragments = append(fragments, ctxcompiler.Fragment{
			ID: fmt.Sprintf("approval-filler-%02d", index), Kind: ctxcompiler.KindConversation,
			Scope: "session", Provenance: "fixture", Trust: "assistant", Version: "v1", Priority: 30,
			Content: fmt.Sprintf("ลำดับ %d ", index) +
				strings.Repeat("บันทึกการสนทนาที่ยาวพอจะกินงบ active slice ทั้งหมด ", pressureFillerRepeats)})
	}
	return CaseInput{Name: "approval-retention", Language: "th", BenchmarkClass: "instruction-retention",
		Fragments: fragments,
		Expectations: Expectations{EssentialIDs: []string{"approval-goal"},
			DecisionIDs: []string{"approval-decision"}, OpenTaskIDs: []string{"approval-open"},
			CausalPairIDs:  []string{"approval-pair"},
			TaskAssertions: []string{"rodmay approved", "waiting on a human decision"}, MaxTaskDelta: 0.05}}
}

const (
	pressureFillerFragments = 80
	pressureFillerRepeats   = 90
)
