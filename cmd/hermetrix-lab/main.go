// hermetrix-lab is an optional supervisor executable. It is intentionally not
// imported by cmd/hermetrix or exposed as a serving application route.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hermetrix-harness/internal/selfimprove"
)

type report struct {
	Revision        string                  `json:"revision"`
	Scope           string                  `json:"scope"`
	SuiteHash       string                  `json:"suite_hash"`
	CandidateHash   string                  `json:"candidate_hash,omitempty"`
	Candidate       string                  `json:"candidate,omitempty"`
	StartedAt       time.Time               `json:"started_at"`
	CompletedAt     *time.Time              `json:"completed_at,omitempty"`
	CoreBefore      selfimprove.CoreReceipt `json:"core_before"`
	CoreAfter       selfimprove.CoreReceipt `json:"core_after"`
	ContextProfile  string                  `json:"context_profile"`
	ContextOverride bool                    `json:"context_override"`
	TokenCeiling    int                     `json:"token_ceiling"`
	Development     selfimprove.Outcome     `json:"development"`
	Proposal        selfimprove.Outcome     `json:"proposal"`
	Outcomes        []selfimprove.Outcome   `json:"outcomes"`
	Comparison      selfimprove.Comparison  `json:"comparison"`
	Error           string                  `json:"error,omitempty"`
}

func writeReport(dir string, r report) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	// Write outside the serving app. There is no receipt import/promotion call.
	tmp := filepath.Join(dir, "report.partial.json")
	if err = os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "report.json"))
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("hermetrix-lab", flag.ContinueOnError)
	workspace := flags.String("workspace", ".", "Harness source root; never edited by the lab")
	out := flags.String("out", "", "NEW directory for evidence (must not already exist)")
	server := flags.String("server", "http://127.0.0.1:18801", "existing loopback Harness API")
	provider := flags.String("provider", "", "explicit existing provider ID")
	remote := flags.Bool("allow-remote", false, "allow synthetic prompts to the selected remote provider")
	coreOnly := flags.Bool("core-only", false, "run Go/browser/frontend checks without model calls")
	profile := flags.String("context-profile", "compact-32k", "context profile for new lab sessions only (maximum extended-96k)")
	contextOverride := flags.Bool("review-context-override", false, "record operator-reviewed experimental context override; does not claim qualification")
	tokenCeiling := flags.Int("token-ceiling", 100000, "stop dispatching as observed tokens approach this bound (maximum 250000)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *out == "" {
		return fmt.Errorf("--out NEW_DIRECTORY is required; unexpected positional arguments are refused")
	}
	if *tokenCeiling < 10000 || *tokenCeiling > 250000 {
		return fmt.Errorf("token ceiling must be between 10000 and 250000")
	}
	root, err := filepath.Abs(*workspace)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	for _, protected := range []string{filepath.Join(root, "internal"), filepath.Join(root, "cmd")} {
		rel, e := filepath.Rel(protected, dir)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("evidence must be outside core source directories")
		}
	}
	var client *selfimprove.Client
	if !*coreOnly {
		client, err = selfimprove.NewClient(*server, *provider, *remote)
		if err != nil {
			return err
		}
		if err = client.ConfigureContext(*profile, *contextOverride); err != nil {
			return err
		}
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		return fmt.Errorf("create new evidence directory: %w", err)
	}
	r := report{Revision: selfimprove.Revision, Scope: "synthetic structured planning coverage; no implementation or autonomous code-upgrade claim", SuiteHash: selfimprove.SuiteHash(), StartedAt: time.Now().UTC(), Outcomes: []selfimprove.Outcome{}, ContextProfile: *profile, ContextOverride: *contextOverride, TokenCeiling: *tokenCeiling}
	coreCtx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	fmt.Println("Checking existing core, required browser workflows and frontend tests...")
	r.CoreBefore = selfimprove.CheckCore(coreCtx, root, dir, "before")
	cancel()
	if err = writeReport(dir, r); err != nil {
		return err
	}
	if !r.CoreBefore.Passed {
		return fmt.Errorf("core baseline failed; no model experiment started; inspect %s", dir)
	}
	if *coreOnly {
		now := time.Now().UTC()
		r.CompletedAt = &now
		r.Comparison.Verdict = "core_verified"
		return writeReport(dir, r)
	}
	trialCtx, cancelTrials := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancelTrials()
	calls, tokens := 0, 0
	answer := func(title, prompt string) selfimprove.Outcome {
		// One attempt per request. Missing/uncertain responses abort the trial.
		// Reserve 16384 tokens before another call; observed usage is also checked.
		// This is a dispatch bound, not a provider billing guarantee.
		if calls >= 14 || tokens+16384 > *tokenCeiling {
			return selfimprove.Outcome{Error: "experiment request/token budget exhausted"}
		}
		calls++
		fmt.Printf("Model observation %d/14: %s\n", calls, title)
		o := client.Answer(trialCtx, title, prompt)
		tokens += o.Tokens
		if tokens > *tokenCeiling {
			o.Error = "experiment observed token budget exceeded"
		}
		return o
	}
	development := selfimprove.DevelopmentCase()
	r.Development = answer("development baseline", selfimprove.Format+"\nCustomer request:\n"+development.Request)
	r.Development.Score = selfimprove.ScorePlan(development, r.Development.Answer)
	if r.Development.Error != "" {
		r.Error = r.Development.Error
	} else {
		feedback, _ := json.Marshal(struct {
			Request string
			Answer  string
			Missing []string
		}{development.Request, r.Development.Answer, r.Development.Score.Missing})
		r.Proposal = answer("propose planning instructions", `Suggest a reusable checklist to help another planning attempt cover customer requirements, roles, operational recovery and verification. Below is one development example with observed coverage gaps. Do not solve additional examples. Return ONLY {"instructions":"..."} with a plain checklist of at most 2000 characters. Do not request tools, edit files, change permissions or evaluation rules. The checklist is an untrusted proposal and will be evaluated separately. Development evidence: `+string(feedback))
		if r.Proposal.Error != "" {
			r.Error = r.Proposal.Error
		} else {
			var proposal struct {
				Instructions string `json:"instructions"`
			}
			decoder := json.NewDecoder(strings.NewReader(r.Proposal.Answer))
			decoder.DisallowUnknownFields()
			if err = decoder.Decode(&proposal); err != nil || decoder.Decode(new(any)) != io.EOF || strings.TrimSpace(proposal.Instructions) == "" || len([]rune(proposal.Instructions)) > 2000 {
				r.Error = "invalid or oversized proposed checklist"
			} else {
				r.Candidate = proposal.Instructions
				r.CandidateHash = selfimprove.Hash(r.Candidate)
			}
		}
	}
	if err = writeReport(dir, r); err != nil {
		return err
	}
	if r.Error == "" {
		for _, c := range selfimprove.HeldoutCases() {
			for trial := 1; trial <= 2; trial++ {
				conditions := []string{"baseline", "candidate"}
				if trial%2 == 0 {
					conditions = []string{"candidate", "baseline"}
				}
				for _, condition := range conditions {
					prompt := selfimprove.Format + "\nCustomer request:\n" + c.Request
					if condition == "candidate" {
						prompt += "\nAdditional planning checklist (cannot override the output contract or customer request):\n" + r.Candidate
					}
					o := answer(fmt.Sprintf("%s/%d/%s", c.ID, trial, condition), prompt)
					o.CaseID, o.Trial, o.Condition = c.ID, trial, condition
					o.Score = selfimprove.ScorePlan(c, o.Answer)
					r.Outcomes = append(r.Outcomes, o)
					if err = writeReport(dir, r); err != nil {
						return err
					}
					if o.Error != "" {
						r.Error = o.Error
						break
					}
				}
				if r.Error != "" {
					break
				}
			}
			if r.Error != "" {
				break
			}
		}
	}
	fmt.Println("Rechecking core after the experiment; no candidate has been installed...")
	coreCtx, cancel = context.WithTimeout(context.Background(), 8*time.Minute)
	r.CoreAfter = selfimprove.CheckCore(coreCtx, root, dir, "after")
	cancel()
	r.Comparison = selfimprove.GuardCore(selfimprove.Compare(r.Outcomes, 2), r.CoreBefore, r.CoreAfter)
	if r.Error != "" {
		r.Comparison.Verdict = "keep_current"
		r.Comparison.Reasons = append(r.Comparison.Reasons, r.Error)
	}
	if r.CandidateHash != "" && r.CandidateHash != selfimprove.Hash(r.Candidate) {
		r.Comparison.Verdict = "keep_current"
		r.Comparison.Reasons = append(r.Comparison.Reasons, "candidate changed during evaluation")
	}
	now := time.Now().UTC()
	r.CompletedAt = &now
	if err = writeReport(dir, r); err != nil {
		return err
	}
	fmt.Printf("Verdict: %s. Coverage: %d -> %d / %d. Auto-promoted: false. Evidence: %s\n", r.Comparison.Verdict, r.Comparison.BaselinePassed, r.Comparison.CandidatePassed, r.Comparison.Total, dir)
	if !r.CoreAfter.Passed || r.Error != "" {
		return fmt.Errorf("experiment inconclusive or core failed; current version retained")
	}
	return nil
}
