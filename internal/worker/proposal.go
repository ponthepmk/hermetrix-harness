// Package worker provides a proposal-only external model boundary. It never
// reads a workspace, executes commands, or applies model-generated changes.
package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"hermetrix-harness/internal/providers"
)

func Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// Run accepts an explicit, caller-curated file bundle, not filesystem paths to
// discover. Successful transport is not verification of the proposed code.
func Run(ctx context.Context, adapter providers.Adapter, profile providers.Profile, key string, task Task) (Result, error) {
	return RunWithOptions(ctx, adapter, profile, key, task, Options{})
}

func RunWithOptions(ctx context.Context, adapter providers.Adapter, profile providers.Profile, key string, task Task, options Options) (Result, error) {
	return runWithOptions(ctx, task, options, func(values ...string) bool {
		return containsCredential(key, values...)
	}, func(ctx context.Context, request providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
		return adapter.StreamChat(ctx, profile, key, request, emit)
	})
}

// RunWithProviderService keeps the credential inside the provider vault while
// preserving the worker's exact-token egress and proposal checks.
func RunWithProviderService(ctx context.Context, service *providers.Service, profile providers.Profile, task Task, options Options) (Result, error) {
	if service == nil {
		return Result{}, workerError(ErrorProviderFailed, PhaseValidatingInput, false, "provider service is unavailable", nil)
	}
	return runWithOptions(ctx, task, options, func(values ...string) bool {
		return service.CredentialAppearsIn(profile, values...)
	}, func(ctx context.Context, request providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
		return service.StreamChat(ctx, profile, request, emit)
	})
}

func runWithOptions(ctx context.Context, task Task, options Options, containsCredentialValue func(...string) bool,
	stream func(context.Context, providers.ChatRequest, func(providers.Delta) error) (providers.Completion, error)) (Result, error) {
	task, body, wireBody, err := prepareWorkerTask(task)
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	sequence := 0
	var progressMu sync.Mutex
	progress := func(phase Phase) {
		progressMu.Lock()
		defer progressMu.Unlock()
		sequence++
		if options.Progress != nil {
			options.Progress(ProgressEvent{Sequence: sequence, Phase: phase, At: now()})
		}
	}
	progress(PhaseValidatingInput)
	if err != nil {
		return Result{}, workerError(ErrorInvalidTask, PhaseValidatingInput, false, err.Error(), nil)
	}
	if strings.TrimSpace(task.Brief) == "" || len(task.Files) == 0 || len(task.Files) > 32 {
		return Result{}, workerError(ErrorInvalidTask, PhaseValidatingInput, false, "brief and 1–32 files required", nil)
	}
	if task.MaxOutputTokens < 0 || task.MaxOutputTokens > 32768 {
		return Result{}, workerError(ErrorInvalidTask, PhaseValidatingInput, false, "max_output_tokens must be between 1 and 32768 when set", nil)
	}
	if len(task.AcceptanceCriteria) > 32 || len(task.Constraints) > 32 || len(task.RecommendedChecks) > 32 {
		return Result{}, workerError(ErrorInvalidTask, PhaseValidatingInput, false, "task lists may contain at most 32 items each", nil)
	}
	if err := validateKnowledgeRefs(task.ProjectBrainRefs); err != nil {
		return Result{}, workerError(ErrorInvalidTask, PhaseValidatingInput, false, "invalid Project Brain references", err)
	}
	if err := validateAbsentFiles(task); err != nil {
		return Result{}, err
	}
	if containsCredentialValue(task.Brief, task.ID, task.Kind) || containsCredentialValue(task.AcceptanceCriteria...) ||
		containsCredentialValue(task.Constraints...) || containsCredentialValue(task.RecommendedChecks...) {
		return Result{}, workerError(ErrorCredentialPresent, PhaseValidatingInput, false, "credential present in task", nil)
	}
	for path := range task.Files {
		if containsCredentialValue(path, task.Files[path]) {
			return Result{}, workerError(ErrorCredentialPresent, PhaseValidatingInput, false, "credential present in task", nil)
		}
		if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.Contains(path, ":") {
			return Result{}, workerError(ErrorInvalidTask, PhaseValidatingInput, false, "invalid relative file path", nil)
		}
		for _, part := range strings.Split(path, "/") {
			if part == "" || part == "." || part == ".." {
				return Result{}, workerError(ErrorInvalidTask, PhaseValidatingInput, false, "invalid relative file path", nil)
			}
		}
	}
	for _, draft := range task.RepairDrafts {
		if containsCredentialValue(draft.Content, draft.SourceArtifactID, draft.ProposalID,
			draft.SourceArtifactSHA256, draft.InputSHA256, draft.SourceSHA256) {
			return Result{}, workerError(ErrorCredentialPresent, PhaseValidatingInput, false, "credential present in repair draft", nil)
		}
	}
	if containsCredentialValue(string(body)) {
		return Result{}, workerError(ErrorCredentialPresent, PhaseValidatingInput, false, "credential present in task", nil)
	}
	request := proposalRequest(task, wireBody)
	if options.BeforeProviderRequest != nil {
		if err := options.BeforeProviderRequest(); err != nil {
			return Result{}, workerError(ErrorProviderFailed, PhaseRequestingProvider, false, "provider dispatch was not authorized", err)
		}
	}
	progress(PhaseRequestingProvider)
	stopHeartbeat := startHeartbeat(ctx, options.HeartbeatInterval, func() { progress(PhaseRequestingProvider) })
	completion, err := stream(ctx, request, func(providers.Delta) error { return nil })
	stopHeartbeat()
	if err != nil {
		return Result{}, providerError(ctx, err)
	}
	if completion.FinishReason == "length" {
		return Result{}, workerError(ErrorResponseTruncated, PhaseRequestingProvider, true, "provider response was truncated", nil)
	}
	progress(PhaseValidatingProposal)
	var raw string
	switch {
	case len(completion.ToolCalls) == 1 && completion.ToolCalls[0].Name == "submit_changes":
		raw = completion.ToolCalls[0].Arguments
	case len(completion.ToolCalls) == 0 && strings.TrimSpace(completion.Content) != "":
		// Some OpenAI-compatible gateways do not preserve native tool calls.
		// Accept only an exact JSON object and run it through the same validator.
		raw = completion.Content
	default:
		return Result{}, invalidProposal("expected one complete submit_changes tool call")
	}
	var proposal struct {
		Summary           string   `json:"summary"`
		Assumptions       []string `json:"assumptions"`
		Risks             []string `json:"risks"`
		RecommendedChecks []string `json:"recommended_checks"`
		Changes           []struct {
			Path        string     `json:"path"`
			Content     *string    `json:"content,omitempty"`
			Edits       []TextEdit `json:"edits,omitempty"`
			RetainDraft *bool      `json:"retain_draft,omitempty"`
		} `json:"changes"`
	}
	if len(raw) > 256*1024 {
		return Result{}, invalidProposal("proposal too large")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return Result{}, invalidProposal("invalid proposal JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Result{}, invalidProposal("trailing proposal data")
	}
	if len(proposal.Changes) == 0 || len(proposal.Changes) > len(task.Files) {
		return Result{}, invalidProposal("invalid change count")
	}
	if len(proposal.Assumptions) > 32 || len(proposal.Risks) > 32 || len(proposal.RecommendedChecks) > 32 {
		return Result{}, invalidProposal("proposal lists may contain at most 32 items each")
	}
	result := Result{Status: "proposed_unverified", TaskID: task.ID, InputSHA256: Hash(string(body)), Summary: proposal.Summary,
		Assumptions: proposal.Assumptions, Risks: proposal.Risks, RecommendedChecks: proposal.RecommendedChecks, Usage: completion.Usage}
	seen := map[string]bool{}
	for _, change := range proposal.Changes {
		before, ok := task.Files[change.Path]
		if !ok || seen[change.Path] {
			return Result{}, invalidProposal("unknown or duplicate proposed path")
		}
		if !writablePathAllowed(task, change.Path) {
			return Result{}, invalidProposal(fmt.Sprintf("proposed path %q is outside the frozen writable paths", change.Path))
		}
		if _, historical := task.RepairDrafts[change.Path]; historical {
			for _, edit := range change.Edits {
				if containsCredentialValue(edit.Old, edit.New) {
					return Result{}, workerError(ErrorCredentialPresent, PhaseValidatingProposal, false, "credential present in draft edit proof", nil)
				}
			}
			derived, err := materializeRepairDraft(task, change.Path, change.Content, change.Edits, change.RetainDraft)
			if err != nil {
				return Result{}, invalidProposal("draft repair rejected: " + err.Error())
			}
			if containsCredentialValue(derived.Content) {
				return Result{}, workerError(ErrorCredentialPresent, PhaseValidatingProposal, false, "credential present in proposal", nil)
			}
			seen[change.Path] = true
			result.Changes = append(result.Changes, derived)
			continue
		}
		if change.RetainDraft != nil {
			return Result{}, invalidProposal("draft retention requires an explicit historical draft")
		}
		content, strategy, err := func() (string, string, error) {
			if view, ok := task.FileViews[change.Path]; ok {
				return MaterializeBoundedChange(change.Path, before, Hash(before), view, change.Content, change.Edits)
			}
			return materializeChange(before, change.Content, change.Edits)
		}()
		if err != nil {
			return Result{}, invalidProposal(fmt.Sprintf("proposal change %q: %s", change.Path, err.Error()))
		}
		if content == before && !absentFile(task, change.Path) {
			return Result{}, invalidProposal(fmt.Sprintf("proposal change %q does not modify the supplied source", change.Path))
		}
		if containsCredentialValue(content) {
			return Result{}, workerError(ErrorCredentialPresent, PhaseValidatingProposal, false, "credential present in proposal", nil)
		}
		seen[change.Path] = true
		beforeHash := Hash(before)
		if absentFile(task, change.Path) {
			if change.Content == nil {
				return Result{}, invalidProposal("new files require complete replacement content")
			}
			beforeHash, strategy = "absent", "create_file"
		}
		if _, ok := task.FileViews[change.Path]; ok {
			outputChange := Change{Path: change.Path, BeforeSHA256: beforeHash, Content: content, Strategy: strategy}
			if len(change.Edits) > 0 {
				outputChange.Edits = make([]TextEdit, len(change.Edits))
				copy(outputChange.Edits, change.Edits)
			}
			result.Changes = append(result.Changes, outputChange)
		} else {
			result.Changes = append(result.Changes, Change{Path: change.Path, BeforeSHA256: beforeHash, Content: content, Strategy: strategy})
		}
	}
	result.ProposalID = proposalID(result.InputSHA256, result)
	if err := validateWorkerResultResources(result); err != nil {
		return Result{}, invalidProposal(err.Error())
	}
	progress(PhaseCompleted)
	return result, nil
}

// ValidateResult rechecks a persisted or transported proposal against the
// exact task contract. A coordinator must call this before turning changes
// into approval-bound write intents; successful validation still does not
// establish that the code is correct.
func ValidateResult(task Task, result Result) error {
	return validateWorkerScopedResult(task, result)
}

func absentFile(task Task, path string) bool {
	for _, p := range task.AbsentFiles {
		if p == path {
			return true
		}
	}
	return false
}

func validateAbsentFiles(task Task) error {
	if len(task.AbsentFiles) > 32 {
		return invalidProposal("too many absent file snapshots")
	}
	seen := map[string]bool{}
	for _, p := range task.AbsentFiles {
		text, ok := task.Files[p]
		if !ok || text != "" || seen[p] || len(task.WritablePaths) == 0 || !writablePathAllowed(task, p) {
			return invalidProposal("absent files require distinct empty snapshots and explicit writable paths")
		}
		seen[p] = true
	}
	return nil
}

func proposalID(inputHash string, result Result) string {
	identity := struct {
		Summary           string   `json:"summary"`
		Assumptions       []string `json:"assumptions"`
		Risks             []string `json:"risks"`
		RecommendedChecks []string `json:"recommended_checks"`
		Changes           []Change `json:"changes"`
	}{result.Summary, result.Assumptions, result.Risks, result.RecommendedChecks, result.Changes}
	encoded, _ := json.Marshal(identity)
	return Hash(inputHash + string(encoded))
}

func writablePathAllowed(task Task, path string) bool {
	if len(task.WritablePaths) == 0 {
		return true // Backward compatibility: all explicitly supplied files.
	}
	for _, allowed := range task.WritablePaths {
		if allowed == path {
			return true
		}
	}
	return false
}

func startHeartbeat(ctx context.Context, interval time.Duration, beat func()) func() {
	if interval <= 0 || beat == nil {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				beat()
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func containsCredential(key string, values ...string) bool {
	if key == "" {
		return false
	}
	for _, value := range values {
		if strings.Contains(value, key) {
			return true
		}
	}
	return false
}

func sliceContainsCredential(key string, values []string) bool {
	return containsCredential(key, values...)
}

func invalidProposal(message string) error {
	return workerError(ErrorInvalidProposal, PhaseValidatingProposal, false, message, nil)
}

func materializeChange(before string, content *string, edits []TextEdit) (string, string, error) {
	if (content == nil) == (len(edits) == 0) {
		return "", "", invalidProposal("each change requires exactly one of content or edits")
	}
	if content != nil {
		return *content, "replace_file", nil
	}
	if len(edits) > 64 {
		return "", "", invalidProposal("a change may contain at most 64 text edits")
	}
	result := before
	for index, edit := range edits {
		if edit.Old == "" {
			return "", "", invalidProposal(fmt.Sprintf("text edit %d has an empty preimage; use complete replacement content", index+1))
		}
		if matches := strings.Count(result, edit.Old); matches != 1 {
			return "", "", invalidProposal(fmt.Sprintf("text edit %d preimage matched %d times; expected exactly once; use complete replacement content or an exact unique preimage", index+1, matches))
		}
		result = strings.Replace(result, edit.Old, edit.New, 1)
	}
	return result, "replace_text", nil
}

func proposalSchema() map[string]any {
	stringList := map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 32}
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"summary": map[string]any{"type": "string"}, "assumptions": stringList, "risks": stringList, "recommended_checks": stringList,
		"changes": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"},
			"edits": map[string]any{"type": "array", "minItems": 1, "maxItems": 64, "items": map[string]any{"type": "object", "properties": map[string]any{"old": map[string]any{"type": "string"}, "new": map[string]any{"type": "string"}}, "required": []string{"old", "new"}, "additionalProperties": false}},
		}, "required": []string{"path"}, "additionalProperties": false}},
	}, "required": []string{"changes"}, "additionalProperties": false}

	schemaProperties := schema["properties"].(map[string]any)
	changesItems := schemaProperties["changes"].(map[string]any)["items"].(map[string]any)
	changesItems["oneOf"] = []map[string]any{
		{"required": []string{"content"}},
		{"required": []string{"edits"}},
	}

	return schema
}
