package web

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	brandassets "hermetrix-harness/assets"
	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/capabilities"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/curator"
	"hermetrix-harness/internal/fidelity"
	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/localmodel"
	"hermetrix-harness/internal/mcp"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/qualification"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

//go:embed ui/*
var uiFiles embed.FS

type Server struct {
	skills    *skills.Service
	learning  *learning.Service
	curator   *curator.Service
	compiler  *ctxcompiler.Compiler
	estimator *ctxcompiler.AdaptiveEstimator
	models    *localmodel.Prober
	providers *providers.Service
	agent     *agent.Service
	mcp       *mcp.Service
	catalog   *capabilities.Catalog
	fidelity  *fidelity.Service
	qualifier *qualification.Service
	product   *product.Service
	store     *store.Store
	logger    *slog.Logger
}

func (s *Server) WithProduct(service *product.Service) *Server {
	s.product = service
	return s
}

func (s *Server) WithMCP(service *mcp.Service, catalog *capabilities.Catalog) *Server {
	s.mcp = service
	s.catalog = catalog
	return s
}

func (s *Server) WithFidelity(service *fidelity.Service) *Server {
	s.fidelity = service
	return s
}

func (s *Server) WithQualification(service *qualification.Service) *Server {
	s.qualifier = service
	return s
}

func New(skillService *skills.Service, learningService *learning.Service, curatorService *curator.Service,
	compiler *ctxcompiler.Compiler, estimator *ctxcompiler.AdaptiveEstimator, models *localmodel.Prober,
	providerService *providers.Service, agentService *agent.Service, dataStore *store.Store,
	logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if models == nil {
		models = localmodel.NewProber()
	}
	return &Server{skills: skillService, learning: learningService, curator: curatorService, compiler: compiler,
		estimator: estimator, models: models, providers: providerService, agent: agentService,
		store: dataStore, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// O-19: this reported a hardcoded 16 while the database was at 17. Health is
	// the one endpoint a client uses to decide whether the server is the one it
	// expects, so a constant written by hand is the least useful thing it can
	// say. It now reports what the open database actually contains.
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		schema, err := s.store.SchemaVersion(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError,
				map[string]any{"ok": false, "service": "hermetrix-harness", "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "hermetrix-harness",
			"schema": schema, "expected_schema": store.CurrentSchemaVersion})
	})
	mux.HandleFunc("GET /api/bootstrap", s.bootstrap)
	mux.HandleFunc("GET /api/skills", s.listSkills)
	mux.HandleFunc("GET /api/skills/{id}", s.getSkill)
	mux.HandleFunc("PATCH /api/skills/{id}", s.updateSkillControls)
	mux.HandleFunc("POST /api/skills/custom", s.createCustomSkill)
	mux.HandleFunc("POST /api/skills/{id}/improvements", s.proposeImprovement)
	mux.HandleFunc("POST /api/skills/{id}/fork", s.forkSkill)
	mux.HandleFunc("POST /api/skills/{id}/archive", s.archiveSkill)
	mux.HandleFunc("GET /api/candidates", s.listCandidates)
	mux.HandleFunc("GET /api/candidates/{id}", s.getCandidate)
	mux.HandleFunc("POST /api/candidates", s.createCandidate)
	mux.HandleFunc("PATCH /api/candidates/{id}", s.updateCandidate)
	mux.HandleFunc("GET /api/candidates/{id}/replays", s.listCandidateReplays)
	mux.HandleFunc("POST /api/candidates/{id}/replays", s.runCandidateReplay)
	mux.HandleFunc("GET /api/candidates/{id}/behavioral-eval", s.getCandidateBehavioralEval)
	mux.HandleFunc("POST /api/candidates/{id}/behavioral-eval", s.recordCandidateBehavioralEval)
	mux.HandleFunc("POST /api/candidates/{id}/capability-review", s.reviewCandidateCapabilities)
	mux.HandleFunc("POST /api/candidates/{id}/promote", s.promoteCandidate)
	mux.HandleFunc("POST /api/candidates/{id}/reject", s.rejectCandidate)
	mux.HandleFunc("GET /api/archives", s.listArchives)
	mux.HandleFunc("POST /api/archives/{id}/restore", s.restoreArchive)
	mux.HandleFunc("GET /api/relations", s.listRelations)
	mux.HandleFunc("POST /api/analysis/relations", s.analyzeRelations)
	mux.HandleFunc("GET /api/skill-authority", s.getSkillAuthority)
	mux.HandleFunc("PUT /api/skill-authority", s.saveSkillAuthority)
	mux.HandleFunc("POST /api/skill-authority/run", s.runSkillAuthority)
	mux.HandleFunc("GET /api/skill-authority/actions", s.listSkillAuthorityActions)
	mux.HandleFunc("POST /api/skill-authority/actions/{id}/rollback", s.rollbackSkillAuthorityAction)
	mux.HandleFunc("GET /api/curator/runs", s.listCuratorRuns)
	mux.HandleFunc("POST /api/curator/run", s.runCurator)
	mux.HandleFunc("POST /api/activations", s.recordActivation)
	mux.HandleFunc("GET /api/reviews", s.listReviews)
	mux.HandleFunc("POST /api/reviews", s.enqueueReview)
	mux.HandleFunc("POST /api/reviews/run-next", s.runNextReview)
	mux.HandleFunc("GET /api/context/profiles", s.contextProfiles)
	mux.HandleFunc("POST /api/context/compile", s.compileContext)
	mux.HandleFunc("POST /api/context/observe", s.observeTokens)
	mux.HandleFunc("GET /api/fidelity/cases", s.listFidelityCases)
	mux.HandleFunc("POST /api/fidelity/cases", s.saveFidelityCase)
	mux.HandleFunc("GET /api/fidelity/runs", s.listFidelityRuns)
	mux.HandleFunc("POST /api/fidelity/cases/{id}/run", s.runFidelityCase)
	mux.HandleFunc("POST /api/local-model/probe", s.probeLocalModel)
	mux.HandleFunc("GET /api/qualifications", s.listQualifications)
	mux.HandleFunc("POST /api/qualifications", s.runQualification)
	mux.HandleFunc("GET /api/providers", s.listProviders)
	mux.HandleFunc("POST /api/providers", s.saveProvider)
	mux.HandleFunc("PUT /api/providers/{id}/credential", s.setProviderCredential)
	mux.HandleFunc("POST /api/providers/{id}/test", s.testProvider)
	mux.HandleFunc("POST /api/providers/{id}/measure-overhead", s.measureProviderOverhead)
	mux.HandleFunc("GET /api/mcp/servers", s.listMCPServers)
	mux.HandleFunc("POST /api/mcp/servers", s.saveMCPServer)
	mux.HandleFunc("PUT /api/mcp/servers/{id}/credential", s.setMCPCredential)
	mux.HandleFunc("POST /api/mcp/servers/{id}/discover", s.discoverMCPServer)
	mux.HandleFunc("GET /api/capabilities", s.listCapabilities)
	mux.HandleFunc("GET /api/capabilities/{id}", s.getCapability)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("POST /api/sessions", s.createSession)
	mux.HandleFunc("GET /api/sessions/{id}", s.getSession)
	mux.HandleFunc("POST /api/sessions/{id}/turns", s.runTurn)
	mux.HandleFunc("POST /api/approvals/{id}/decisions", s.decideApproval)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.deleteSession)
	mux.HandleFunc("GET /api/filesystem/directories", s.browseDirectories)
	mux.HandleFunc("GET /api/elicitations", s.listElicitations)
	mux.HandleFunc("POST /api/elicitations/{id}/answer", s.answerElicitation)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.saveProject)
	mux.HandleFunc("PUT /api/projects/{id}/pin", s.pinProject)
	mux.HandleFunc("POST /api/projects/{id}/open", s.openProject)
	mux.HandleFunc("GET /api/projects/{id}/files", s.browseProject)
	mux.HandleFunc("GET /api/projects/{id}/file", s.readProjectFile)
	mux.HandleFunc("PUT /api/projects/{id}/file", s.writeProjectFile)
	mux.HandleFunc("POST /api/projects/{id}/commands", s.startProjectCommand)
	mux.HandleFunc("GET /api/terminals", s.listTerminals)
	mux.HandleFunc("POST /api/terminals", s.startTerminal)
	mux.HandleFunc("GET /api/terminals/{id}/output", s.terminalOutput)
	mux.HandleFunc("POST /api/terminals/{id}/input", s.writeTerminal)
	mux.HandleFunc("POST /api/terminals/{id}/resize", s.resizeTerminal)
	mux.HandleFunc("POST /api/terminals/{id}/close", s.closeTerminal)
	mux.HandleFunc("GET /api/browser/tabs", s.listBrowserTabs)
	mux.HandleFunc("POST /api/browser/tabs", s.openBrowserTab)
	mux.HandleFunc("POST /api/browser/tabs/{id}/actions", s.browserAction)
	mux.HandleFunc("GET /api/teams", s.listAgentTeams)
	mux.HandleFunc("POST /api/teams", s.saveAgentTeam)
	mux.HandleFunc("GET /api/team-runs", s.listTeamRuns)
	mux.HandleFunc("POST /api/team-runs", s.startTeamRun)
	mux.HandleFunc("GET /api/team-runs/{id}", s.getTeamRun)
	mux.HandleFunc("POST /api/team-runs/{id}/cancel", s.cancelTeamRun)
	mux.HandleFunc("POST /api/team-runs/{id}/tasks/{task}/approval", s.decideTeamTaskApproval)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.cancelJob)
	mux.HandleFunc("GET /api/artifacts", s.listArtifacts)
	mux.HandleFunc("POST /api/artifacts", s.createArtifact)
	mux.HandleFunc("POST /api/deliverables", s.createDeliverable)
	mux.HandleFunc("GET /api/artifacts/{id}/content", s.getArtifactContent)
	mux.HandleFunc("GET /api/settings", s.listSettings)
	mux.HandleFunc("PUT /api/settings", s.saveSetting)
	mux.HandleFunc("GET /api/memories", s.listMemories)
	mux.HandleFunc("POST /api/memories", s.saveMemory)
	mux.HandleFunc("POST /api/memories/{id}/archive", s.archiveMemory)
	mux.HandleFunc("GET /api/usage", s.usageSummary)
	mux.HandleFunc("GET /api/skill-retrieval", s.skillRetrievalMetrics)
	mux.HandleFunc("GET /api/token-accuracy", s.tokenAccuracyMetrics)
	mux.HandleFunc("GET /api/backups", s.listBackups)
	mux.HandleFunc("POST /api/backups", s.exportBackup)
	mux.HandleFunc("GET /api/backups/{id}/download", s.downloadBackup)
	mux.HandleFunc("POST /api/imports/preview", s.previewImport)
	mux.HandleFunc("POST /api/imports/{id}/apply", s.applyImport)
	mux.HandleFunc("GET /api/curator/findings", s.listCuratorFindings)
	mux.HandleFunc("GET /api/maintenance/schedules", s.listSchedules)
	mux.HandleFunc("POST /api/maintenance/schedules", s.saveSchedule)
	mux.HandleFunc("GET /api/maintenance/system-state", s.systemState)
	mux.HandleFunc("POST /api/maintenance/run-due", s.runDueMaintenance)
	mux.HandleFunc("GET /api/maintenance/gc", s.listGCRuns)
	mux.HandleFunc("POST /api/maintenance/gc/dry-run", s.dryRunGC)
	mux.HandleFunc("POST /api/maintenance/gc/{id}/apply", s.applyGC)
	mux.HandleFunc("POST /api/maintenance/gc/{id}/restore", s.restoreGC)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(brandassets.Files))))
	assets, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err)
	}
	// O-12: without this, any /api/ path that matches no route -- a typo, or a
	// real route called with the wrong method -- falls through to the SPA and
	// answers 200 with HTML. A client cannot tell that apart from success, which
	// is the transport-level version of claiming a result with no receipt.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "no API route matches " + r.Method + " " + r.URL.Path})
	})
	mux.Handle("/", spa(http.FileServer(http.FS(assets)), assets))
	return requestLog(s.logger, securityHeaders(mux))
}

func (s *Server) listQualifications(w http.ResponseWriter, r *http.Request) {
	if s.qualifier == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "model qualification service is unavailable"})
		return
	}
	items, err := s.qualifier.List(r.Context(), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) runQualification(w http.ResponseWriter, r *http.Request) {
	if s.qualifier == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "model qualification service is unavailable"})
		return
	}
	var input qualification.Input
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.qualifier.Run(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listFidelityCases(w http.ResponseWriter, r *http.Request) {
	if s.fidelity == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "context fidelity service is unavailable"})
		return
	}
	items, err := s.fidelity.ListCases(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveFidelityCase(w http.ResponseWriter, r *http.Request) {
	if s.fidelity == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "context fidelity service is unavailable"})
		return
	}
	var input fidelity.CaseInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.fidelity.SaveCase(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listFidelityRuns(w http.ResponseWriter, r *http.Request) {
	if s.fidelity == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "context fidelity service is unavailable"})
		return
	}
	items, err := s.fidelity.ListRuns(r.Context(), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) runFidelityCase(w http.ResponseWriter, r *http.Request) {
	if s.fidelity == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "context fidelity service is unavailable"})
		return
	}
	var input struct {
		ProfileName string `json:"profile_name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.fidelity.Run(r.Context(), r.PathValue("id"), input.ProfileName)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	skillList, err := s.skills.ListSkills(ctx, false)
	if err != nil {
		writeError(w, err)
		return
	}
	candidates, err := s.skills.ListCandidates(ctx, "")
	if err != nil {
		writeError(w, err)
		return
	}
	archives, err := s.skills.ListArchives(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	relations, err := s.skills.ListRelations(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	reviews, err := s.learning.List(ctx, "")
	if err != nil {
		writeError(w, err)
		return
	}
	curatorRuns, err := s.curator.ListRuns(ctx, 20)
	if err != nil {
		writeError(w, err)
		return
	}
	providerProfiles := []providers.Profile{}
	if s.providers != nil {
		providerProfiles, err = s.providers.List(ctx)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	mcpServers := []mcp.Server{}
	if s.mcp != nil {
		mcpServers, err = s.mcp.List(ctx)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	capabilitySummary := capabilities.Summary{BySource: map[string]int{}, ByReadiness: map[string]int{}}
	if s.catalog != nil {
		capabilitySummary = s.catalog.Summary()
	}
	sessions := []agent.Session{}
	if s.agent != nil {
		sessions, err = s.agent.ListSessions(ctx)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": skillList, "candidates": candidates,
		"archives": archives, "relations": relations, "profiles": ctxcompiler.Profiles(),
		"reviews": reviews, "curator_runs": curatorRuns, "providers": providerProfiles, "sessions": sessions,
		"mcp_servers": mcpServers, "capability_summary": capabilitySummary,
		"estimator_multiplier": s.estimator.Multiplier()})
}

func (s *Server) listMCPServers(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "MCP service is unavailable"})
		return
	}
	items, err := s.mcp.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "MCP service is unavailable"})
		return
	}
	// Same shape as a provider: the bearer token is part of connecting, and it
	// goes to the credential vault rather than into SQLite or this response.
	var input struct {
		mcp.SaveInput
		APIKey string `json:"api_key"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.mcp.Save(r.Context(), input.SaveInput)
	if err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(input.APIKey) != "" {
		updated, credentialErr := s.mcp.SetCredential(r.Context(), item.ID, input.APIKey)
		if credentialErr != nil {
			writeError(w, credentialErr)
			return
		}
		item = updated
	}
	writeJSON(w, http.StatusCreated, item)
}

// setMCPCredential stores or clears one server's bearer token.
func (s *Server) setMCPCredential(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "MCP service is unavailable"})
		return
	}
	var input struct {
		APIKey string `json:"api_key"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.mcp.SetCredential(r.Context(), r.PathValue("id"), input.APIKey)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) discoverMCPServer(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "MCP service is unavailable"})
		return
	}
	result, err := s.mcp.Discover(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "capability catalog is unavailable"})
		return
	}
	limit := 20
	if len([]rune(r.URL.Query().Get("query"))) > 512 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "query must be at most 512 characters"})
		return
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 50 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit must be between 1 and 50"})
			return
		}
		limit = value
	}
	items := s.catalog.Search(r.URL.Query().Get("query"), r.URL.Query().Get("source"), limit)
	writeJSON(w, http.StatusOK, map[string]any{"results": items, "count": len(items), "schemas_exposed": false})
}

func (s *Server) getCapability(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "capability catalog is unavailable"})
		return
	}
	item, err := s.catalog.Describe(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	items, err := s.skills.ListSkills(r.Context(), r.URL.Query().Get("include_archived") == "true")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	item, err := s.skills.GetSkill(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	version, err := s.skills.GetVersion(r.Context(), item.CurrentVersionID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": item, "version": version})
}

func (s *Server) createCandidate(w http.ResponseWriter, r *http.Request) {
	var input skills.CreateCandidateInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.CreateCandidate(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) createCustomSkill(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CanonicalName string   `json:"canonical_name"`
		ScopeKind     string   `json:"scope_kind"`
		ScopeRef      string   `json:"scope_ref"`
		Reason        string   `json:"reason"`
		Markdown      string   `json:"markdown"`
		EvidenceRefs  []string `json:"evidence_refs"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.CreateCandidate(r.Context(), skills.CreateCandidateInput{CanonicalName: input.CanonicalName,
		ScopeKind: input.ScopeKind, ScopeRef: input.ScopeRef, Origin: "user_created", Owner: "user", ChangeKind: "create",
		CreatedBy: "local-user", TriggerKind: "custom_skill_studio", Reason: input.Reason,
		EvidenceRefs: append([]string{"ui:skill-studio"}, input.EvidenceRefs...), Markdown: input.Markdown})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateSkillControls(w http.ResponseWriter, r *http.Request) {
	var input skills.UpdateSkillControlsInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.UpdateSkillControls(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateCandidate(w http.ResponseWriter, r *http.Request) {
	var input skills.UpdateCandidateInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.UpdateCandidate(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) proposeImprovement(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor  string `json:"actor"`
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.ProposeImprovement(r.Context(), r.PathValue("id"), input.Actor, input.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) forkSkill(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CanonicalName string `json:"canonical_name"`
		Actor         string `json:"actor"`
		Reason        string `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.ForkSkill(r.Context(), r.PathValue("id"), input.CanonicalName, input.Actor, input.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getSkillAuthority(w http.ResponseWriter, r *http.Request) {
	policy, err := s.skills.GetAuthorityPolicy(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (s *Server) saveSkillAuthority(w http.ResponseWriter, r *http.Request) {
	var input skills.SaveAuthorityPolicyInput
	if !decodeJSON(w, r, &input) {
		return
	}
	policy, err := s.skills.SaveAuthorityPolicy(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (s *Server) runSkillAuthority(w http.ResponseWriter, r *http.Request) {
	actions, err := s.skills.ProcessPendingAuthority(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, actions)
}

func (s *Server) listSkillAuthorityActions(w http.ResponseWriter, r *http.Request) {
	actions, err := s.skills.ListAuthorityActions(r.Context(), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, actions)
}

func (s *Server) rollbackSkillAuthorityAction(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor  string `json:"actor"`
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	candidate, err := s.skills.CreateAuthorityRollback(r.Context(), r.PathValue("id"), input.Actor, input.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, candidate)
}

func (s *Server) listCandidates(w http.ResponseWriter, r *http.Request) {
	items, err := s.skills.ListCandidates(r.Context(), r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getCandidate(w http.ResponseWriter, r *http.Request) {
	item, err := s.skills.GetCandidate(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listCandidateReplays(w http.ResponseWriter, r *http.Request) {
	items, err := s.skills.ListCandidateReplays(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// getCandidateBehavioralEval reports the current evaluation state, including
// "not_run", so an operator can see why promotion is blocked without having to
// try it.
func (s *Server) getCandidateBehavioralEval(w http.ResponseWriter, r *http.Request) {
	item, err := s.skills.LatestBehavioralEval(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// recordCandidateBehavioralEval stores the result of an evaluation run.
//
// The verdict is derived from the numbers rather than accepted from the caller,
// so a client cannot post "passed" beside a regression. Promotion of an
// improvement is blocked until a passing evaluation exists for that exact
// candidate revision and hash.
func (s *Server) recordCandidateBehavioralEval(w http.ResponseWriter, r *http.Request) {
	var input skills.BehavioralEvalInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.CandidateID = r.PathValue("id")
	item, err := s.skills.RecordBehavioralEval(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) runCandidateReplay(w http.ResponseWriter, r *http.Request) {
	item, err := s.skills.RunCandidateReplay(r.Context(), r.PathValue("id"))
	if err != nil && item.ID == "" {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, item)
}

func (s *Server) reviewCandidateCapabilities(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor            string `json:"actor"`
		Decision         string `json:"decision"`
		ExpectedRevision int    `json:"expected_revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.ReviewCandidateCapabilities(r.Context(), r.PathValue("id"), input.ExpectedRevision,
		input.Actor, input.Decision)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) promoteCandidate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor            string `json:"actor"`
		ExpectedRevision int    `json:"expected_revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.PromoteCandidate(r.Context(), r.PathValue("id"), input.Actor, input.ExpectedRevision)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) rejectCandidate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor            string `json:"actor"`
		Reason           string `json:"reason"`
		ExpectedRevision int    `json:"expected_revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.skills.RejectCandidate(r.Context(), r.PathValue("id"), input.Actor, input.Reason, input.ExpectedRevision); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) archiveSkill(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor          string `json:"actor"`
		Reason         string `json:"reason"`
		AbsorbedIntoID string `json:"absorbed_into_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.ArchiveSkill(r.Context(), r.PathValue("id"), input.Actor, input.Reason, input.AbsorbedIntoID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listArchives(w http.ResponseWriter, r *http.Request) {
	items, err := s.skills.ListArchives(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) restoreArchive(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor  string `json:"actor"`
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.skills.RestoreArchive(r.Context(), r.PathValue("id"), input.Actor, input.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listRelations(w http.ResponseWriter, r *http.Request) {
	items, err := s.skills.ListRelations(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) analyzeRelations(w http.ResponseWriter, r *http.Request) {
	_, err := s.curator.RunReportOnly(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := s.skills.ListRelations(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) listCuratorRuns(w http.ResponseWriter, r *http.Request) {
	items, err := s.curator.ListRuns(r.Context(), 50)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) runCurator(w http.ResponseWriter, r *http.Request) {
	run, err := s.curator.RunReportOnly(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	actions, err := s.curator.ApplyConfiguredAuthority(r.Context(), run.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run, "authority_actions": actions})
}

func (s *Server) recordActivation(w http.ResponseWriter, r *http.Request) {
	var input skills.ActivationInput
	if !decodeJSON(w, r, &input) {
		return
	}
	id, err := s.skills.RecordActivation(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	items, err := s.learning.List(r.Context(), r.URL.Query().Get("state"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) enqueueReview(w http.ResponseWriter, r *http.Request) {
	var input learning.EnqueueInput
	if !decodeJSON(w, r, &input) {
		return
	}
	job, duplicate, err := s.learning.Enqueue(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job": job, "duplicate": duplicate})
}

func (s *Server) runNextReview(w http.ResponseWriter, r *http.Request) {
	job, err := s.learning.RunNext(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) contextProfiles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"profiles": ctxcompiler.Profiles(), "estimator_multiplier": s.estimator.Multiplier()})
}

func (s *Server) compileContext(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProfileName        string                 `json:"profile_name"`
		Fragments          []ctxcompiler.Fragment `json:"fragments"`
		DirectTools        []ctxcompiler.ToolSpec `json:"direct_tools"`
		WorstCaseToolBurst int                    `json:"worst_case_tool_burst"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	profile, ok := profileByName(input.ProfileName)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown context profile"})
		return
	}
	result, err := s.compiler.Compile(r.Context(), ctxcompiler.Request{Profile: profile, Fragments: input.Fragments,
		DirectTools: input.DirectTools, WorstCaseToolBurst: input.WorstCaseToolBurst})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) observeTokens(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Predicted int `json:"predicted"`
		Actual    int `json:"actual"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	s.estimator.Observe(input.Predicted, input.Actual)
	writeJSON(w, http.StatusOK, map[string]any{"multiplier": s.estimator.Multiplier()})
}

func (s *Server) probeLocalModel(w http.ResponseWriter, r *http.Request) {
	var input localmodel.ProbeRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.models.Probe(r.Context(), input)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusOK, []providers.Profile{})
		return
	}
	items, err := s.providers.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveProvider(w http.ResponseWriter, r *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "provider service is unavailable"})
		return
	}
	// The API key rides along with the profile so connecting a provider is one
	// action in the UI. It is stored in the credential vault and never written
	// to SQLite, a log line or this response.
	var input struct {
		providers.SaveInput
		APIKey string `json:"api_key"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.providers.Save(r.Context(), input.SaveInput)
	if err != nil {
		writeError(w, err)
		return
	}
	if strings.TrimSpace(input.APIKey) != "" {
		updated, credentialErr := s.providers.SetCredential(r.Context(), item.ID, input.APIKey)
		if credentialErr != nil {
			writeError(w, credentialErr)
			return
		}
		item = updated
	}
	writeJSON(w, http.StatusCreated, item)
}

// setProviderCredential stores or clears one provider's API key. An empty
// token clears it, which is the only way to remove a key from the UI.
func (s *Server) setProviderCredential(w http.ResponseWriter, r *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "provider service is unavailable"})
		return
	}
	var input struct {
		APIKey string `json:"api_key"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.providers.SetCredential(r.Context(), r.PathValue("id"), input.APIKey)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "provider service is unavailable"})
		return
	}
	result, err := s.providers.Test(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// deleteSession removes one conversation. What was learned from it stays: the
// Skill activations, reviews and token observations it produced are evidence
// about Skills and models, not about the chat, and its artifacts are detached
// rather than destroyed.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	if err := s.agent.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, agent.ErrSessionRunning) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// browseDirectories backs the folder picker on the Projects screen. A browser
// will not hand a web page an absolute path, so registering a project root has
// to walk the filesystem here. It answers directory names only, and the control
// server refuses to listen anywhere but loopback.
func (s *Server) browseDirectories(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	listing, err := s.product.BrowseDirectories(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

// listElicitations returns the questions an MCP server is waiting on right now.
// They live in memory rather than the database on purpose: a question is only
// answerable while the server that asked it is still on the other end of the
// call, and a restart ends that.
func (s *Server) listElicitations(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusOK, []agent.PendingElicitation{})
		return
	}
	writeJSON(w, http.StatusOK, s.agent.PendingElicitations(r.URL.Query().Get("session_id")))
}

// answerElicitation delivers the user's reply to the waiting server.
func (s *Server) answerElicitation(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	var input agent.ElicitationAnswer
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.agent.AnswerElicitation(r.PathValue("id"), input); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"answered": true})
}

// skillRetrievalMetrics reports the ADR-7 exit criterion: whether models
// actually call skill_search when a matching Skill is available to them.
func (s *Server) skillRetrievalMetrics(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusOK, []agent.SkillRetrievalStats{})
		return
	}
	items, err := s.agent.SkillRetrievalMetrics(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusOK, []agent.Session{})
		return
	}
	items, err := s.agent.ListSessions(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	var input agent.CreateSessionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.agent.CreateSession(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	item, err := s.agent.GetSessionDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) runTurn(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	var input agent.TurnInput
	if !decodeJSON(w, r, &input) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming is unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	lastType := ""
	_, err := s.agent.RunTurn(r.Context(), r.PathValue("id"), input, func(event agent.StreamEvent) error {
		lastType = event.Type
		if err := encoder.Encode(event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil && lastType != "failed" {
		_ = encoder.Encode(agent.StreamEvent{Type: "failed", Error: fmt.Sprintf("turn failed: %v", err)})
		flusher.Flush()
	}
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	var input agent.ApprovalDecisionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming is unavailable"})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	lastType := ""
	_, err := s.agent.DecideApproval(r.Context(), r.PathValue("id"), input, func(event agent.StreamEvent) error {
		lastType = event.Type
		if err := encoder.Encode(event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil && lastType != "failed" {
		_ = encoder.Encode(agent.StreamEvent{Type: "failed", Error: fmt.Sprintf("approval decision failed: %v", err)})
		flusher.Flush()
	}
}

func profileByName(name string) (ctxcompiler.Profile, bool) {
	return ctxcompiler.ProfileByName(name)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON: " + err.Error()})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, capabilities.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, capabilities.ErrRevisionConflict):
		status = http.StatusConflict
	case errors.Is(err, skills.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, skills.ErrRevisionConflict), errors.Is(err, skills.ErrCandidateNotReady):
		status = http.StatusConflict
	case errors.Is(err, skills.ErrProtectedSkill), errors.Is(err, skills.ErrChecksFailed), errors.Is(err, skills.ErrImmutableMetadata),
		errors.Is(err, skills.ErrForkRequired), errors.Is(err, skills.ErrReplayRequired), errors.Is(err, skills.ErrCapabilityReview):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, ctxcompiler.ErrDirectToolsOverflow), errors.Is(err, ctxcompiler.ErrPinnedOverflow), errors.Is(err, ctxcompiler.ErrContextOverflow):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, learning.ErrNoQueuedReview):
		status = http.StatusConflict
	default:
		var mcpErr *mcp.Error
		if errors.As(err, &mcpErr) {
			switch mcpErr.Kind {
			case mcp.ErrorConfiguration, mcp.ErrorProtocol:
				status = http.StatusUnprocessableEntity
			case mcp.ErrorNotReady:
				status = http.StatusConflict
			case mcp.ErrorTimeout:
				status = http.StatusGatewayTimeout
			case mcp.ErrorCancelled:
				status = 499
			case mcp.ErrorTransport, mcp.ErrorRemote:
				status = http.StatusBadGateway
			case mcp.ErrorRevision:
				status = http.StatusConflict
			case mcp.ErrorPolicy:
				status = http.StatusForbidden
			}
			break
		}
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "missing") {
			status = http.StatusBadRequest
		}
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(normalizeJSONList(value))
}

// normalizeJSONList encodes an empty list as [] rather than null. Go marshals a
// nil slice as null, and every list endpoint here is read by the cockpit as an
// array. On a fresh data directory /api/terminals, /api/browser/tabs,
// /api/teams and /api/team-runs all returned null, which threw inside the
// cockpit's initial load and left every panel unrendered -- the UI looked like
// it had failed to start rather than like it had nothing to show.
func normalizeJSONList(value any) any {
	if value == nil {
		return value
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Slice && reflected.IsNil() {
		return reflect.MakeSlice(reflected.Type(), 0, 0).Interface()
	}
	return value
}

func spa(next http.Handler, assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(assets, name); err != nil {
			r.URL.Path = "/"
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Debug("http request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) tokenAccuracyMetrics(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	items, err := s.agent.TokenAccuracyMetrics(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"band": agent.TokenAccuracyBand,
		"minimum_samples": agent.TokenAccuracyMinimumSamples, "models": items})
}

func (s *Server) measureProviderOverhead(w http.ResponseWriter, r *http.Request) {
	item, err := s.providers.MeasureTokenOverhead(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
