package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

// runCorpus exports and scores the digest corpus behind the Phase 8
// semantic-reviewer gate. It is a command rather than an HTTP route because it
// reads and writes a directory of case files a person edits by hand, and
// because scoring spends a model call per case.
func runCorpus(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected export or score")
	}
	switch args[0] {
	case "export":
		return corpusExport(args[1:])
	case "score":
		return corpusScore(args[1:])
	default:
		return fmt.Errorf("unknown corpus command %q", args[0])
	}
}

// corpusExport writes committed reviews out as unlabelled cases.
//
// Exporting without a label is deliberate: the label is the judgement this
// corpus exists to capture, and pre-filling it from what the reviewer already
// decided would make the gate measure the reviewer against itself.
//
// Selection groups by trigger family and then by evidence shape, and draws
// round-robin across a family's shapes. A hundred and eighty-two milestone
// digests collapsed to twelve shapes with one holding a hundred and seven:
// taken whole that would put most of the corpus on a single easy negative and
// make the false-proposal ceiling trivially satisfiable. Capping the shape
// instead starves a family whose cases are all one shape -- repeated_correction
// has two, and a per-shape cap of five left it with six cases against a target
// of twenty-five. Round-robin covers the variety a family actually has and caps
// the family, which is the thing worth capping.
func corpusExport(args []string) error {
	flags := flag.NewFlagSet("corpus export", flag.ExitOnError)
	dataRoot := flags.String("data", ".hermetrix", "local data directory")
	out := flags.String("out", "corpus/digests", "directory to write case files into")
	labeller := flags.String("labeller", "", "name recorded on cases you are about to label")
	perFamily := flags.Int("per-family", 0,
		"keep at most this many cases per trigger family, drawn round-robin across that family's shapes (0 keeps all)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	dataStore, err := store.Open(ctx, *dataRoot)
	if err != nil {
		return err
	}
	defer dataStore.Close()
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	rows, err := dataStore.DB.QueryContext(ctx,
		`SELECT id, trigger_kind, digest_json FROM learning_reviews ORDER BY created_at`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pending struct {
		id     string
		digest learning.Digest
	}
	byShape := map[string]map[string][]pending{}
	shapeOrder := map[string][]string{}
	families := []string{}
	skipped := 0
	for rows.Next() {
		var id, family, digestJSON string
		if err := rows.Scan(&id, &family, &digestJSON); err != nil {
			return err
		}
		var digest learning.Digest
		if err := json.Unmarshal([]byte(digestJSON), &digest); err != nil {
			return fmt.Errorf("review %s: %w", id, err)
		}
		if _, err := os.Stat(filepath.Join(*out, id+".json")); err == nil {
			// Never overwrite: a case on disk may already carry a label.
			skipped++
			continue
		}
		if byShape[family] == nil {
			byShape[family] = map[string][]pending{}
			families = append(families, family)
		}
		shape := digestShape(digest)
		if _, seen := byShape[family][shape]; !seen {
			shapeOrder[family] = append(shapeOrder[family], shape)
		}
		byShape[family][shape] = append(byShape[family][shape], pending{id: id, digest: digest})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Strings(families)
	written, setAside := 0, 0
	coverage := map[string]int{}
	for _, family := range families {
		available := 0
		for _, bucket := range byShape[family] {
			available += len(bucket)
		}
		taken := 0
		for cursor := 0; ; cursor++ {
			progressed := false
			for _, shape := range shapeOrder[family] {
				if *perFamily > 0 && taken >= *perFamily {
					break
				}
				bucket := byShape[family][shape]
				if cursor >= len(bucket) {
					continue
				}
				progressed = true
				item := bucket[cursor]
				raw, err := json.MarshalIndent(learning.CorpusCase{ID: item.id, TriggerKind: family,
					Provenance: "driven", SourceReviewID: item.id, Digest: item.digest,
					Label: learning.Label{Rationale: "TODO", LabeledBy: *labeller,
						LabeledAt: time.Now().UTC().Format("2006-01-02")}}, "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(*out, item.id+".json"), append(raw, '\n'), 0o600); err != nil {
					return err
				}
				coverage[family]++
				written++
				taken++
			}
			if !progressed || (*perFamily > 0 && taken >= *perFamily) {
				break
			}
		}
		setAside += available - taken
	}
	fmt.Printf("wrote %d cases, skipped %d already on disk", written, skipped)
	if setAside > 0 {
		fmt.Printf(", set aside %d beyond %d per family", setAside, *perFamily)
	}
	fmt.Println()
	fmt.Println("family coverage of what was exported:")
	for _, family := range sortedKeys(coverage) {
		fmt.Printf("  %-22s %d\n", family, coverage[family])
	}
	for _, family := range []string{"successful_milestone", "explicit_learn", "repeated_correction", "skill_failure"} {
		if coverage[family] < learning.CorpusMinimumCasesPerFamily {
			fmt.Printf("  short of %d cases for %s; that family has to be driven, not harvested\n",
				learning.CorpusMinimumCasesPerFamily, family)
		}
	}
	if *labeller == "" {
		fmt.Println("note: --labeller was empty, so these cases will not load until someone signs the labels")
	}
	return nil
}

func corpusScore(args []string) error {
	flags := flag.NewFlagSet("corpus score", flag.ExitOnError)
	dataRoot := flags.String("data", ".hermetrix", "local data directory")
	dir := flags.String("dir", "corpus/digests", "directory of labelled case files")
	providerName := flags.String("provider", "", "provider profile to review with; defaults to the first enabled")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cases, err := learning.LoadCorpus(*dir)
	if err != nil {
		return err
	}
	if len(cases) == 0 {
		return fmt.Errorf("no cases in %s", *dir)
	}
	ctx := context.Background()
	dataStore, err := store.Open(ctx, *dataRoot)
	if err != nil {
		return err
	}
	defer dataStore.Close()
	providerService := providers.NewService(dataStore, providers.NewOpenAIAdapter(nil))
	profile, err := selectProvider(ctx, providerService, *providerName)
	if err != nil {
		return err
	}
	reviewer := learning.NewModelReviewer(providerService, profile.ID)
	fmt.Printf("scoring %d cases against %s (%s)\n\n", len(cases), profile.Name, profile.Model)
	coverage := learning.FamilyCoverage(cases)
	fmt.Println("family coverage:")
	for _, family := range sortedKeys(coverage) {
		note := ""
		if coverage[family] < learning.CorpusMinimumCasesPerFamily {
			note = fmt.Sprintf("  (under %d)", learning.CorpusMinimumCasesPerFamily)
		}
		fmt.Printf("  %-22s %d%s\n", family, coverage[family], note)
	}
	results, err := learning.ScoreCorpus(ctx, reviewer, cases)
	if err != nil {
		return err
	}
	fmt.Println()
	failed := false
	for _, result := range results {
		fmt.Printf("%-10s cases %3d  should propose %3d  proposed %3d  rate %.2f  false %d (%.2f)  invented %d  reviewer errors %d  -> %s\n",
			result.Provenance, result.Cases, result.ShouldPropose, result.Proposed, result.ProposalRate,
			result.FalseProposals, result.FalseProposalRate, result.InventedEvidence,
			result.ReviewerErrors, result.Verdict)
		if result.Verdict == "failed" {
			failed = true
		}
	}
	for _, result := range results {
		if len(result.Failures) == 0 {
			continue
		}
		fmt.Printf("\n%s failures:\n", result.Provenance)
		for _, failure := range result.Failures {
			fmt.Println("  " + failure)
		}
	}
	if failed {
		return fmt.Errorf("corpus gate failed")
	}
	return nil
}

func selectProvider(ctx context.Context, service *providers.Service, name string) (providers.Profile, error) {
	items, err := service.List(ctx)
	if err != nil {
		return providers.Profile{}, err
	}
	for _, item := range items {
		if name == "" && item.Enabled {
			return item, nil
		}
		if strings.EqualFold(item.Name, name) {
			return item, nil
		}
	}
	return providers.Profile{}, fmt.Errorf("no provider profile matched %q", name)
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// digestShape is what makes two digests the same kind of evidence: which tools
// ran, whether the user pushed back, and whether a Skill was in play. Two
// digests with the same shape ask the reviewer the same question.
func digestShape(digest learning.Digest) string {
	tools := map[string]bool{}
	for _, receipt := range digest.ToolReceipts {
		parts := strings.Split(receipt, ":")
		if len(parts) >= 4 {
			tools[parts[len(parts)-2]] = true
		}
	}
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Sprintf("%s|corrections=%t|skills=%t", strings.Join(names, ","),
		len(digest.UserCorrections) > 0, len(digest.SkillActivations) > 0)
}
