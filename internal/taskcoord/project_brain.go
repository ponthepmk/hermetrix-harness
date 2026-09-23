package taskcoord

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/worker"
)

// KnowledgeRetriever is intentionally read-only. The local task coordinator
// cannot ask Pi to grant an effect or change a StepPacket.
type KnowledgeRetriever interface {
	Retrieve(context.Context, string) ([]ctxcompiler.Fragment, error)
}

// WithProjectBrain binds one local project to one explicit Pi project. It does
// not infer a Pi scope from a filesystem path or from the task's text.
func (s *Service) WithProjectBrain(localProjectID, piProject string, retriever KnowledgeRetriever,
	compiler *ctxcompiler.Compiler) *Service {
	s.brainLocalProjectID = strings.TrimSpace(localProjectID)
	s.brainProject = strings.TrimSpace(piProject)
	s.brain = retriever
	s.brainCompiler = compiler
	return s
}

func (s *Service) projectBrainRefs(ctx context.Context, localProjectID, query string,
	profile providers.Profile) []worker.KnowledgeRef {
	if s == nil || s.brain == nil || s.brainCompiler == nil || s.brainProject == "" ||
		localProjectID == "" || localProjectID != s.brainLocalProjectID || profile.ContextWindow < 16384 {
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	fragments, err := s.brain.Retrieve(lookupCtx, query)
	if err != nil {
		// A read-only Pi outage never turns a healthy local task into a failed
		// task. Do not log the query or a remote response that might be sensitive.
		slog.WarnContext(ctx, "Project Brain unavailable for durable task; continuing locally", "code", "project_brain_read_failed")
		return nil
	}
	if len(fragments) == 0 {
		return nil
	}
	// The existing compiler decides which external references fit. The
	// StepPacket and its hash remain untouched: only the proposal model sees
	// these optional, low-trust context fragments.
	compiled, err := s.brainCompiler.Compile(lookupCtx,
		ctxcompiler.Request{Profile: ctxcompiler.Compact16K(), Fragments: fragments})
	if err != nil {
		slog.WarnContext(ctx, "Project Brain context did not fit durable task budget; continuing locally", "code", "project_brain_compile_failed")
		return nil
	}
	refs := make([]worker.KnowledgeRef, 0, 3)
	total := 0
	for _, fragment := range compiled.Fragments {
		if fragment.Kind != ctxcompiler.KindProjectKnowledge || fragment.Scope != "project_brain:"+s.brainProject ||
			fragment.Provenance != "pi-second-brain" || fragment.Trust != "external_curated_not_verified" ||
			fragment.Pinned || len(fragment.Content) > 5000 || total+len(fragment.Content) > 6000 {
			continue
		}
		// A compaction summary has no exact citation. Only unchanged source
		// fragments may be passed to the worker as version-bound evidence.
		original := false
		for _, candidate := range fragments {
			if candidate.ID == fragment.ID && candidate.Version == fragment.Version && candidate.Content == fragment.Content {
				original = true
				break
			}
		}
		if !original {
			continue
		}
		refs = append(refs, worker.KnowledgeRef{Citation: fragment.ID, Version: fragment.Version,
			Provenance: fragment.Provenance, Trust: fragment.Trust, Content: fragment.Content})
		total += len(fragment.Content)
		if len(refs) == 3 {
			break
		}
	}
	if len(refs) == 0 {
		return nil
	}
	encoded, err := json.Marshal(refs)
	if err != nil || len(encoded) > 8*1024 || s.providers.CredentialAppearsIn(profile, string(encoded)) {
		slog.WarnContext(ctx, "Project Brain references did not pass local input checks; continuing locally", "code", "project_brain_input_rejected")
		return nil
	}
	return refs
}

func knowledgeRefIdentities(refs []worker.KnowledgeRef) []map[string]string {
	items := make([]map[string]string, 0, len(refs))
	for _, ref := range refs {
		items = append(items, map[string]string{"citation": ref.Citation, "version": ref.Version})
	}
	return items
}
