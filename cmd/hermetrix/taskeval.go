package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskeval"
)

func runTaskEval(args []string) {
	if len(args) == 0 {
		usage()
	}
	var err error
	switch args[0] {
	case "generate":
		err = taskEvalGenerate(args[1:])
	case "score":
		err = taskEvalScore(args[1:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func taskEvalGenerate(args []string) error {
	flags := flag.NewFlagSet("taskeval generate", flag.ExitOnError)
	dir := flags.String("dir", "corpus/tasks", "directory to write task files into")
	perClass := flags.Int("per-class", taskeval.MinimumTasksPerClass,
		"tasks per class; the gate's floor is the default")
	seed := flags.Int64("seed", 1, "placement sampler seed, so a corpus can be rebuilt exactly")
	noise := flags.Int("noise", taskeval.DefaultNoiseFragments,
		"surrounding history fragments per task; the default keeps the full-context "+
			"condition inside a 96k provider window while still forcing compaction")
	if err := flags.Parse(args); err != nil {
		return err
	}
	tasks, err := taskeval.Generate(taskeval.GenerateOptions{PerClass: *perClass, Seed: *seed,
		NoiseFragments: *noise})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	placements := map[string]int{}
	for _, task := range tasks {
		placements[task.Placement]++
		encoded, err := json.MarshalIndent(task, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(*dir, task.ID+".json"), encoded, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d tasks to %s\n", len(tasks), *dir)
	fmt.Printf("placement mix: head %d, tail %d, middle %d (%.1f%% middle against a measured %.1f%%)\n",
		placements[taskeval.PlacementHead], placements[taskeval.PlacementTail],
		placements[taskeval.PlacementMiddle],
		100*float64(placements[taskeval.PlacementMiddle])/float64(len(tasks)),
		100*taskeval.MiddlePlacementRate)
	return nil
}

// completionAnswerer adapts the provider service to the runner's Answerer.
// StreamChat is safe for concurrent use, which the runner requires.
type completionAnswerer struct {
	service   *providers.Service
	profile   providers.Profile
	maxTokens int
}

func (a completionAnswerer) Answer(ctx context.Context, messages []providers.Message) (providers.Completion, error) {
	return a.service.StreamChat(ctx, a.profile, providers.ChatRequest{
		Messages: messages, MaxTokens: a.maxTokens}, func(providers.Delta) error { return nil })
}

func taskEvalScore(args []string) error {
	flags := flag.NewFlagSet("taskeval score", flag.ExitOnError)
	dataRoot := flags.String("data", ".hermetrix", "local data directory")
	dir := flags.String("dir", "corpus/tasks", "directory of task files")
	providerName := flags.String("provider", "", "provider profile to answer with; defaults to the first enabled")
	profileName := flags.String("profile", "compact-32k", "context profile to compile against")
	concurrency := flags.Int("concurrency", taskeval.DefaultConcurrency,
		"how many requests to keep in flight; the time is queueing, not compute")
	maxTokens := flags.Int("max-tokens", 512, "answer budget per request")
	out := flags.String("out", "", "optional path to write the full JSON report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	tasks, err := taskeval.LoadCorpus(*dir)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no tasks in %s", *dir)
	}
	contextProfile, ok := ctxcompiler.ProfileByName(*profileName)
	if !ok {
		return fmt.Errorf("unknown context profile %q", *profileName)
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
	compiler := ctxcompiler.NewCompiler(ctxcompiler.NewAdaptiveEstimator(),
		ctxcompiler.NewBlobSpiller(dataStore.Blobs),
		ctxcompiler.NewVerifiedCompactor(ctxcompiler.StructuredCompactor{}))
	runner := taskeval.NewRunner(compiler, contextProfile,
		completionAnswerer{service: providerService, profile: profile, maxTokens: *maxTokens})
	runner.Concurrency = *concurrency
	// The full-context condition has to be a request the provider will accept,
	// or the comparison is an answer against an error.
	runner.FullContextCeiling = profile.ContextWindow - profile.MaxOutputTokens

	fmt.Printf("scoring %d tasks (%d requests) against %s (%s) at %s\n\n",
		len(tasks), len(tasks)*2, profile.Name, profile.Model, contextProfile.Name)
	started := time.Now()
	runner.Progress = func(done, total int, outcome taskeval.Outcome) {
		elapsed := time.Since(started)
		remaining := time.Duration(0)
		if done > 0 {
			remaining = time.Duration(float64(elapsed) / float64(done) * float64(total-done))
		}
		fmt.Fprintf(os.Stderr, "\r  answered %d/%d  elapsed %s  eta %s  %-28s",
			done, total, elapsed.Round(time.Second), remaining.Round(time.Second),
			outcome.TaskID+" "+outcome.Condition)
	}
	report, err := runner.Run(ctx, tasks)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	fmt.Println()
	for _, class := range report.Classes {
		fmt.Printf("%-14s tasks %3d  full %.2f  compiled %.2f  delta %+.3f (max %.2f)  "+
			"false success %d->%d  facts reachable %d  -> %s\n",
			class.Class, class.Tasks, class.SuccessFull, class.SuccessCompiled, class.SuccessDelta,
			class.Tolerance, class.FalseSuccessFull, class.FalseSuccessCompiled,
			class.FactsReachable, class.Verdict)
		if class.Note != "" {
			fmt.Printf("%-14s   %s\n", "", class.Note)
		}
	}
	if *out != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, encoded, 0o644); err != nil {
			return err
		}
		fmt.Printf("\nfull report written to %s\n", *out)
	}
	fmt.Println()
	if !report.Passed {
		return fmt.Errorf("task-success gate did not pass")
	}
	fmt.Println("task-success gate passed")
	return nil
}
