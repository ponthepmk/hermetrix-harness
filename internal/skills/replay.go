package skills

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"hermetrix-harness/internal/identity"
)

const replayRunnerRevision = "skill-replay-v1"

type ReplayFixture struct {
	ID               string   `json:"id"`
	Prompt           string   `json:"prompt"`
	RequiredPhrases  []string `json:"required_phrases"`
	ForbiddenPhrases []string `json:"forbidden_phrases"`
	RequiredTools    []string `json:"required_tools"`
}

type ReplayCaseResult struct {
	ID                  string   `json:"id"`
	FixturePath         string   `json:"fixture_path"`
	BaselinePassed      bool     `json:"baseline_passed"`
	CandidatePassed     bool     `json:"candidate_passed"`
	FixtureStrengthened bool     `json:"fixture_strengthened"`
	Missing             []string `json:"missing,omitempty"`
	ForbiddenFound      []string `json:"forbidden_found,omitempty"`
	MissingTools        []string `json:"missing_tools,omitempty"`
	FixtureError        string   `json:"fixture_error,omitempty"`
}

type ReplaySummary struct {
	Passed        bool     `json:"passed"`
	Regressions   int      `json:"regressions"`
	Improvements  int      `json:"improvements"`
	AddedTools    []string `json:"added_tools,omitempty"`
	WeakenedTests []string `json:"weakened_tests,omitempty"`
	// AuthorFixtures counts the cases someone actually wrote. When a Skill has
	// none, an improve replay still runs, against a single synthesised fixture
	// asserting the manifest name, description and tool list are unchanged.
	//
	// That check is worth running and it is not a behavioural one. A candidate
	// replacing "keep amounts in satang, round half up" with "use floating point
	// baht, round down and ignore the remainder" passes it, because the manifest
	// is identical. Reported as fixtures_total: 1, summary passed, replay_passed
	// true, it reads like a test suite found no regression.
	AuthorFixtures int `json:"author_fixtures"`
	// ImplicitOnly says the run had nothing but that synthesised fixture, so a
	// reviewer can see how much the green result is worth.
	ImplicitOnly bool `json:"implicit_only,omitempty"`
}

type ReplayRun struct {
	ID                string             `json:"id"`
	CandidateID       string             `json:"candidate_id"`
	CandidateRevision int                `json:"candidate_revision"`
	CandidateHash     string             `json:"candidate_hash"`
	BaseVersionID     string             `json:"base_version_id,omitempty"`
	RunnerRevision    string             `json:"runner_revision"`
	State             string             `json:"state"`
	FixturesTotal     int                `json:"fixtures_total"`
	BaselinePassed    int                `json:"baseline_passed"`
	CandidatePassed   int                `json:"candidate_passed"`
	Regressions       int                `json:"regressions"`
	Improvements      int                `json:"improvements"`
	Summary           ReplaySummary      `json:"summary"`
	Cases             []ReplayCaseResult `json:"cases"`
	Diff              string             `json:"diff"`
	Error             string             `json:"error,omitempty"`
	StartedAt         time.Time          `json:"started_at"`
	CompletedAt       *time.Time         `json:"completed_at,omitempty"`
}

type CapabilityReview struct {
	CandidateID       string    `json:"candidate_id"`
	CandidateRevision int       `json:"candidate_revision"`
	Actor             string    `json:"actor"`
	Decision          string    `json:"decision"`
	AddedTools        []string  `json:"added_tools"`
	CreatedAt         time.Time `json:"created_at"`
}

type fixtureSource struct {
	Path    string
	Fixture ReplayFixture
}

func requireReplayForChange(changeKind string, checks CheckSet) CheckSet {
	if changeKind != "improve" {
		return checks
	}
	checks.ReplayRequired = true
	checks.ReplayPassed = false
	checks.Findings = append(checks.Findings, CheckFinding{Level: "error", Code: "replay_required",
		Message: "run deterministic baseline/candidate replay before promotion", Path: "tests/"})
	checks.Passed = false
	checks.CheckerVersion = "hermetrix-checks-v2"
	return checks
}

func (s *Service) RunCandidateReplay(ctx context.Context, candidateID string) (ReplayRun, error) {
	candidate, err := s.GetCandidate(ctx, candidateID)
	if err != nil {
		return ReplayRun{}, err
	}
	if candidate.State != CandidateNeedsReview && candidate.State != CandidateQuarantined {
		return ReplayRun{}, ErrCandidateNotReady
	}
	candidatePackage, err := s.packageByRef(candidate.CandidateBlobRef)
	if err != nil {
		return ReplayRun{}, err
	}
	basePackage := Package{Format: 1, Files: []File{{Path: "SKILL.md", Content: nil}}}
	if candidate.BaseVersionID != "" {
		base, loadErr := s.GetVersion(ctx, candidate.BaseVersionID)
		if loadErr != nil {
			return ReplayRun{}, loadErr
		}
		basePackage, err = s.packageByRef(base.PackageBlobRef)
		if err != nil {
			return ReplayRun{}, err
		}
	}
	now := time.Now().UTC()
	run := ReplayRun{ID: identity.New("replay"), CandidateID: candidate.ID, CandidateRevision: candidate.Revision,
		CandidateHash: candidate.CandidateHash, BaseVersionID: candidate.BaseVersionID, RunnerRevision: replayRunnerRevision,
		State: "running", StartedAt: now, Diff: boundedLineDiff(basePackage.Markdown(), candidatePackage.Markdown())}
	if _, err := s.store.DB.ExecContext(ctx, `INSERT INTO skill_replay_runs(id,candidate_id,candidate_revision,
      candidate_hash,base_version_id,runner_revision,state,diff_text,started_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		run.ID, run.CandidateID, run.CandidateRevision, run.CandidateHash, run.BaseVersionID, run.RunnerRevision,
		run.State, run.Diff, formatTime(now)); err != nil {
		return ReplayRun{}, err
	}

	baseFixtures, baseFixtureErr := parseReplayFixtures(basePackage)
	candidateFixtures, candidateFixtureErr := parseReplayFixtures(candidatePackage)
	if baseFixtureErr != nil || candidateFixtureErr != nil {
		message := strings.TrimSpace(strings.Join(nonEmpty(errorString(baseFixtureErr), errorString(candidateFixtureErr)), "; "))
		return s.finishReplayFailure(context.WithoutCancel(ctx), run, candidate, message)
	}
	fixtures, weakened := mergeReplayFixtures(baseFixtures, candidateFixtures)
	authorFixtures := len(fixtures)
	if candidate.ChangeKind == "improve" && len(fixtures) == 0 {
		manifest := ParseManifest(basePackage.Markdown())
		fixture := ReplayFixture{ID: "_implicit_manifest_contract", Prompt: "Preserve the active manifest contract",
			RequiredPhrases: []string{manifest.Name, manifest.Description}, RequiredTools: manifest.Tools}
		fixtures = []fixtureSource{{Path: "harness://implicit/baseline-manifest", Fixture: fixture}}
	}
	baseManifest := ParseManifest(basePackage.Markdown())
	candidateManifest := ParseManifest(candidatePackage.Markdown())
	for _, source := range fixtures {
		basePass, _, _, _ := evaluateFixture(source.Fixture, basePackage.Markdown(), baseManifest)
		candidatePass, missing, forbidden, missingTools := evaluateFixture(source.Fixture, candidatePackage.Markdown(), candidateManifest)
		caseResult := ReplayCaseResult{ID: source.Fixture.ID, FixturePath: source.Path, BaselinePassed: basePass,
			CandidatePassed: candidatePass, FixtureStrengthened: fixtureWasStrengthened(source.Fixture.ID, baseFixtures, candidateFixtures),
			Missing: missing, ForbiddenFound: forbidden, MissingTools: missingTools}
		run.Cases = append(run.Cases, caseResult)
		if basePass {
			run.BaselinePassed++
		}
		if candidatePass {
			run.CandidatePassed++
		}
		if basePass && !candidatePass {
			run.Regressions++
		}
		if !basePass && candidatePass {
			run.Improvements++
		}
	}
	run.FixturesTotal = len(run.Cases)
	addedTools := difference(candidateManifest.Tools, baseManifest.Tools)
	run.Summary = ReplaySummary{Passed: run.CandidatePassed == run.FixturesTotal && run.Regressions == 0 && len(weakened) == 0,
		Regressions: run.Regressions, Improvements: run.Improvements, AddedTools: addedTools, WeakenedTests: weakened,
		AuthorFixtures: authorFixtures, ImplicitOnly: authorFixtures == 0 && run.FixturesTotal > 0}
	return s.finishReplay(context.WithoutCancel(ctx), run, candidate)
}

func (s *Service) finishReplayFailure(ctx context.Context, run ReplayRun, candidate Candidate, message string) (ReplayRun, error) {
	run.State, run.Error = "failed", message
	run.Summary.Passed = false
	completed := time.Now().UTC()
	run.CompletedAt = &completed
	resultJSON, _ := json.Marshal(run.Summary)
	_, persistErr := s.store.DB.ExecContext(ctx, `UPDATE skill_replay_runs SET state='failed',result_json=?,error=?,completed_at=? WHERE id=?`,
		string(resultJSON), message, formatTime(completed), run.ID)
	if persistErr == nil {
		persistErr = s.applyReplayChecks(ctx, candidate, run)
	}
	if persistErr != nil {
		return ReplayRun{}, persistErr
	}
	return run, fmt.Errorf("replay failed: %s", message)
}

func (s *Service) finishReplay(ctx context.Context, run ReplayRun, candidate Candidate) (ReplayRun, error) {
	completed := time.Now().UTC()
	run.CompletedAt = &completed
	if run.Summary.Passed {
		run.State = "completed"
	} else {
		run.State = "failed"
		run.Error = "candidate replay did not satisfy every fixture without regression"
	}
	resultJSON, _ := json.Marshal(run.Summary)
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return ReplayRun{}, err
	}
	defer tx.Rollback()
	for _, item := range run.Cases {
		details, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT INTO skill_replay_cases(run_id,case_id,fixture_path,baseline_passed,
        candidate_passed,details_json) VALUES(?,?,?,?,?,?)`, run.ID, item.ID, item.FixturePath,
			item.BaselinePassed, item.CandidatePassed, string(details)); err != nil {
			return ReplayRun{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE skill_replay_runs SET state=?,fixtures_total=?,baseline_passed=?,
      candidate_passed=?,regressions=?,improvements=?,result_json=?,error=?,completed_at=? WHERE id=?`, run.State,
		run.FixturesTotal, run.BaselinePassed, run.CandidatePassed, run.Regressions, run.Improvements,
		string(resultJSON), run.Error, formatTime(completed), run.ID); err != nil {
		return ReplayRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return ReplayRun{}, err
	}
	if err := s.applyReplayChecks(ctx, candidate, run); err != nil {
		return ReplayRun{}, err
	}
	return run, nil
}

func (s *Service) applyReplayChecks(ctx context.Context, candidate Candidate, run ReplayRun) error {
	current, err := s.GetCandidate(ctx, candidate.ID)
	if err != nil {
		return err
	}
	if current.Revision != run.CandidateRevision || current.CandidateHash != run.CandidateHash {
		return ErrRevisionConflict
	}
	checks := current.Checks
	filtered := checks.Findings[:0]
	for _, finding := range checks.Findings {
		if finding.Code != "replay_required" && finding.Code != "replay_failed" &&
			finding.Code != "replay_implicit_only" {
			filtered = append(filtered, finding)
		}
	}
	checks.Findings = filtered
	checks.ReplayRequired = current.ChangeKind == "improve"
	checks.ReplayPassed = run.State == "completed" && run.Summary.Passed
	if checks.ReplayRequired && !checks.ReplayPassed {
		checks.Findings = append(checks.Findings, CheckFinding{Level: "error", Code: "replay_failed", Message: run.Error, Path: "tests/"})
	}
	// A green replay over no author-written fixture is a manifest-identity
	// check. Say so on the candidate, where the person approving it looks,
	// rather than only in the run they may never open.
	if checks.ReplayPassed && run.Summary.ImplicitOnly {
		checks.Findings = append(checks.Findings, CheckFinding{Level: "warning", Code: "replay_implicit_only",
			Message: "no replay fixture exists for this Skill; the run only confirmed the manifest name, " +
				"description and tool list are unchanged, and did not test the procedure", Path: "tests/"})
	}
	checks.Passed = checks.LintPassed && checks.SecurityPassed && (!checks.ReplayRequired || checks.ReplayPassed)
	state := CandidateNeedsReview
	if !checks.Passed {
		state = CandidateQuarantined
	}
	encoded, _ := json.Marshal(checks)
	result, err := s.store.DB.ExecContext(ctx, `UPDATE skill_candidates SET checks_json=?,state=?,updated_at=?
      WHERE id=? AND revision=? AND candidate_hash=? AND state IN (?,?)`, string(encoded), state,
		formatTime(time.Now().UTC()), current.ID, current.Revision, current.CandidateHash, CandidateNeedsReview, CandidateQuarantined)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (s *Service) ListCandidateReplays(ctx context.Context, candidateID string) ([]ReplayRun, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT id,candidate_id,candidate_revision,candidate_hash,base_version_id,
      runner_revision,state,fixtures_total,baseline_passed,candidate_passed,regressions,improvements,result_json,
      diff_text,error,started_at,completed_at FROM skill_replay_runs WHERE candidate_id=? ORDER BY started_at DESC`, candidateID)
	if err != nil {
		return nil, err
	}
	runs := []ReplayRun{}
	for rows.Next() {
		run, err := scanReplayRun(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range runs {
		cases, err := s.replayCases(ctx, runs[index].ID)
		if err != nil {
			return nil, err
		}
		runs[index].Cases = cases
	}
	return runs, nil
}

func (s *Service) ReviewCandidateCapabilities(ctx context.Context, candidateID string, revision int, actor, decision string) (CapabilityReview, error) {
	actor = strings.TrimSpace(actor)
	decision = strings.ToLower(strings.TrimSpace(decision))
	if actor == "" || (decision != "approve" && decision != "deny") {
		return CapabilityReview{}, fmt.Errorf("actor and approve/deny decision are required")
	}
	candidate, err := s.GetCandidate(ctx, candidateID)
	if err != nil {
		return CapabilityReview{}, err
	}
	if revision != candidate.Revision {
		return CapabilityReview{}, ErrRevisionConflict
	}
	added, err := s.addedCapabilities(ctx, candidate)
	if err != nil {
		return CapabilityReview{}, err
	}
	if len(added) == 0 {
		return CapabilityReview{}, fmt.Errorf("candidate does not widen declared capabilities")
	}
	now := time.Now().UTC()
	encoded, _ := json.Marshal(added)
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO candidate_capability_reviews(candidate_id,candidate_revision,actor,
      decision,added_tools_json,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(candidate_id,candidate_revision)
      DO UPDATE SET actor=excluded.actor,decision=excluded.decision,added_tools_json=excluded.added_tools_json,
      created_at=excluded.created_at`, candidate.ID, candidate.Revision, actor, decision, string(encoded), formatTime(now))
	if err != nil {
		return CapabilityReview{}, err
	}
	return CapabilityReview{CandidateID: candidate.ID, CandidateRevision: candidate.Revision, Actor: actor,
		Decision: decision, AddedTools: added, CreatedAt: now}, nil
}

func (s *Service) requireCurrentReplay(ctx context.Context, candidate Candidate) error {
	var state, hash string
	var revision int
	err := s.store.DB.QueryRowContext(ctx, `SELECT state,candidate_hash,candidate_revision FROM skill_replay_runs
      WHERE candidate_id=? ORDER BY started_at DESC LIMIT 1`, candidate.ID).Scan(&state, &hash, &revision)
	if errors.Is(err, sql.ErrNoRows) || state != "completed" || hash != candidate.CandidateHash || revision != candidate.Revision {
		return ErrReplayRequired
	}
	return err
}

func (s *Service) requireCapabilityReview(ctx context.Context, candidate Candidate, candidatePackage Package) error {
	added, err := s.addedCapabilitiesFromPackage(ctx, candidate, candidatePackage)
	if err != nil || len(added) == 0 {
		return err
	}
	var decision, encoded string
	err = s.store.DB.QueryRowContext(ctx, `SELECT decision,added_tools_json FROM candidate_capability_reviews
      WHERE candidate_id=? AND candidate_revision=?`, candidate.ID, candidate.Revision).Scan(&decision, &encoded)
	if err != nil || decision != "approve" {
		return ErrCapabilityReview
	}
	var reviewed []string
	_ = json.Unmarshal([]byte(encoded), &reviewed)
	if strings.Join(cleanStrings(reviewed), "\x00") != strings.Join(cleanStrings(added), "\x00") {
		return ErrCapabilityReview
	}
	return nil
}

func (s *Service) addedCapabilities(ctx context.Context, candidate Candidate) ([]string, error) {
	pkg, err := s.packageByRef(candidate.CandidateBlobRef)
	if err != nil {
		return nil, err
	}
	return s.addedCapabilitiesFromPackage(ctx, candidate, pkg)
}

func (s *Service) addedCapabilitiesFromPackage(ctx context.Context, candidate Candidate, candidatePackage Package) ([]string, error) {
	if candidate.BaseVersionID == "" {
		return nil, nil
	}
	base, err := s.GetVersion(ctx, candidate.BaseVersionID)
	if err != nil {
		return nil, err
	}
	return difference(ParseManifest(candidatePackage.Markdown()).Tools, base.Manifest.Tools), nil
}

func parseReplayFixtures(pkg Package) (map[string]fixtureSource, error) {
	fixtures := map[string]fixtureSource{}
	for _, file := range pkg.Files {
		if !strings.HasPrefix(file.Path, "tests/") || !strings.HasSuffix(strings.ToLower(file.Path), ".json") {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(file.Content))
		decoder.DisallowUnknownFields()
		var fixture ReplayFixture
		if err := decoder.Decode(&fixture); err != nil {
			return nil, fmt.Errorf("%s: %w", file.Path, err)
		}
		if trailingErr := decoder.Decode(&struct{}{}); !errors.Is(trailingErr, io.EOF) {
			return nil, fmt.Errorf("%s: multiple JSON values are not allowed", file.Path)
		}
		fixture.ID = strings.TrimSpace(fixture.ID)
		fixture.Prompt = strings.TrimSpace(fixture.Prompt)
		fixture.RequiredPhrases = cleanStrings(fixture.RequiredPhrases)
		fixture.ForbiddenPhrases = cleanStrings(fixture.ForbiddenPhrases)
		fixture.RequiredTools = cleanStrings(fixture.RequiredTools)
		if fixture.ID == "" || fixture.Prompt == "" || utf8.RuneCountInString(fixture.ID) > 120 ||
			(len(fixture.RequiredPhrases)+len(fixture.ForbiddenPhrases)+len(fixture.RequiredTools) == 0) {
			return nil, fmt.Errorf("%s: id, prompt, and at least one assertion are required", file.Path)
		}
		if _, exists := fixtures[fixture.ID]; exists {
			return nil, fmt.Errorf("duplicate replay fixture id %q", fixture.ID)
		}
		fixtures[fixture.ID] = fixtureSource{Path: file.Path, Fixture: fixture}
		if len(fixtures) > 100 {
			return nil, fmt.Errorf("skill package exceeds 100 replay fixtures")
		}
	}
	return fixtures, nil
}

func mergeReplayFixtures(base, candidate map[string]fixtureSource) ([]fixtureSource, []string) {
	merged := map[string]fixtureSource{}
	for id, item := range base {
		merged[id] = item
	}
	for id, item := range candidate {
		merged[id] = item
	}
	weakened := []string{}
	for id, baseItem := range base {
		candidateItem, exists := candidate[id]
		if !exists || !fixtureContains(candidateItem.Fixture, baseItem.Fixture) {
			weakened = append(weakened, id)
		}
	}
	items := make([]fixtureSource, 0, len(merged))
	for _, item := range merged {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Fixture.ID < items[j].Fixture.ID })
	sort.Strings(weakened)
	return items, weakened
}

func fixtureContains(candidate, base ReplayFixture) bool {
	return isSuperset(candidate.RequiredPhrases, base.RequiredPhrases) &&
		isSuperset(candidate.ForbiddenPhrases, base.ForbiddenPhrases) &&
		isSuperset(candidate.RequiredTools, base.RequiredTools)
}

func fixtureWasStrengthened(id string, base, candidate map[string]fixtureSource) bool {
	before, beforeOK := base[id]
	after, afterOK := candidate[id]
	if !afterOK {
		return false
	}
	if !beforeOK {
		return true
	}
	return len(after.Fixture.RequiredPhrases)+len(after.Fixture.ForbiddenPhrases)+len(after.Fixture.RequiredTools) >
		len(before.Fixture.RequiredPhrases)+len(before.Fixture.ForbiddenPhrases)+len(before.Fixture.RequiredTools)
}

func evaluateFixture(fixture ReplayFixture, markdown string, manifest Manifest) (bool, []string, []string, []string) {
	content := strings.ToLower(markdown)
	missing, forbidden, missingTools := []string{}, []string{}, []string{}
	for _, phrase := range fixture.RequiredPhrases {
		if !strings.Contains(content, strings.ToLower(phrase)) {
			missing = append(missing, phrase)
		}
	}
	for _, phrase := range fixture.ForbiddenPhrases {
		if strings.Contains(content, strings.ToLower(phrase)) {
			forbidden = append(forbidden, phrase)
		}
	}
	tools := termSlice(manifest.Tools)
	for _, tool := range fixture.RequiredTools {
		if !tools[strings.ToLower(tool)] {
			missingTools = append(missingTools, tool)
		}
	}
	return len(missing)+len(forbidden)+len(missingTools) == 0, missing, forbidden, missingTools
}

func (s *Service) replayCases(ctx context.Context, runID string) ([]ReplayCaseResult, error) {
	rows, err := s.store.DB.QueryContext(ctx, `SELECT details_json FROM skill_replay_cases WHERE run_id=? ORDER BY case_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ReplayCaseResult{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item ReplayCaseResult
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type replayScanner interface{ Scan(...any) error }

func scanReplayRun(row replayScanner) (ReplayRun, error) {
	var run ReplayRun
	var summaryJSON, started string
	var completed sql.NullString
	if err := row.Scan(&run.ID, &run.CandidateID, &run.CandidateRevision, &run.CandidateHash, &run.BaseVersionID,
		&run.RunnerRevision, &run.State, &run.FixturesTotal, &run.BaselinePassed, &run.CandidatePassed,
		&run.Regressions, &run.Improvements, &summaryJSON, &run.Diff, &run.Error, &started, &completed); err != nil {
		return ReplayRun{}, err
	}
	_ = json.Unmarshal([]byte(summaryJSON), &run.Summary)
	run.StartedAt, _ = parseTime(started)
	if completed.Valid {
		value, _ := parseTime(completed.String)
		run.CompletedAt = &value
	}
	return run, nil
}

func difference(values, base []string) []string {
	known := termSlice(base)
	out := []string{}
	for _, value := range cleanStrings(values) {
		if !known[strings.ToLower(value)] {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func isSuperset(values, base []string) bool {
	set := termSlice(values)
	for _, value := range base {
		if !set[strings.ToLower(strings.TrimSpace(value))] {
			return false
		}
	}
	return true
}

func termSlice(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = true
	}
	return out
}

func boundedLineDiff(before, after string) string {
	if before == after {
		return "  (no content change)"
	}
	left, right := strings.Split(before, "\n"), strings.Split(after, "\n")
	prefix := 0
	for prefix < len(left) && prefix < len(right) && left[prefix] == right[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(left)-prefix && suffix < len(right)-prefix && left[len(left)-1-suffix] == right[len(right)-1-suffix] {
		suffix++
	}
	lines := []string{}
	start := prefix - 3
	if start < 0 {
		start = 0
	}
	for _, line := range left[start:prefix] {
		lines = append(lines, "  "+line)
	}
	for _, line := range left[prefix : len(left)-suffix] {
		lines = append(lines, "- "+line)
	}
	for _, line := range right[prefix : len(right)-suffix] {
		lines = append(lines, "+ "+line)
	}
	end := len(right) - suffix + 3
	if end > len(right) {
		end = len(right)
	}
	for _, line := range right[len(right)-suffix : end] {
		lines = append(lines, "  "+line)
	}
	if len(lines) > 500 {
		lines = append(lines[:480], "… diff clipped at 500 lines …")
	}
	return strings.Join(lines, "\n")
}

func nonEmpty(values ...string) []string {
	out := []string{}
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
