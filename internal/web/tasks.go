package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"hermetrix-harness/internal/taskcoord"
	"hermetrix-harness/internal/taskengine"
)

func (s *Server) requireTaskEngine(w http.ResponseWriter) bool {
	if s.tasks == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "durable task engine is unavailable"})
		return false
	}
	return true
}

func (s *Server) listDurableTasks(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.tasks.List(r.Context(), r.URL.Query().Get("project_id"), limit)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createDurableTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input taskengine.CreateTaskInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.tasks.Create(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getDurableTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	item, err := s.tasks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) getTaskNextPacket(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	packet, err := s.tasks.BuildNextStepPacket(r.Context(), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, packet)
}

func (s *Server) getTaskExecution(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	snapshot, err := s.tasks.Execution(r.Context(), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) beginTaskRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		ExpectedTaskRevision int    `json:"expected_task_revision"`
		Owner                string `json:"owner"`
		LeaseSeconds         int    `json:"lease_seconds"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	run, err := s.tasks.BeginRun(r.Context(), taskengine.BeginRunInput{
		TaskID:               r.PathValue("id"),
		ExpectedTaskRevision: input.ExpectedTaskRevision,
		Owner:                input.Owner,
		LeaseDuration:        time.Duration(input.LeaseSeconds) * time.Second,
	})
	if err != nil {
		taskError(w, err)
		return
	}
	task, err := s.tasks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"run": run, "task": task})
}

func (s *Server) autoPlanTask(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input taskcoord.AutoPlanInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.TaskID = r.PathValue("id")
	input.Actor = effectiveActor(r.Context(), input.Actor)
	output, err := s.coord.AutoPlan(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, output)
}

func (s *Server) renewTaskRunLease(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		LeaseToken   string `json:"lease_token"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	run, err := s.tasks.RenewRunLease(r.Context(), r.PathValue("id"), input.LeaseToken, time.Duration(input.LeaseSeconds)*time.Second)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) beginTaskAttempt(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input taskengine.BeginAttemptInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.RunID = r.PathValue("id")
	attempt, err := s.tasks.BeginStepAttempt(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, attempt)
}

func (s *Server) planTaskEffect(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		Action       string                  `json:"action"`
		Target       string                  `json:"target"`
		Authority    string                  `json:"authority"`
		RunAuthority taskengine.RunAuthority `json:"run_authority"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	effect, err := s.tasks.PlanEffect(r.Context(), input.RunAuthority, r.PathValue("id"), input.Action, input.Target, input.Authority)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, effect)
}

func (s *Server) createTaskProposal(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input taskcoord.ProposalInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.AttemptID = r.PathValue("id")
	output, err := s.coord.Propose(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, output)
}

func (s *Server) selectTaskFiles(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input taskcoord.SelectFilesInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.AttemptID = r.PathValue("id")
	output, err := s.coord.SelectFiles(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, output)
}

func (s *Server) getTaskCodeProposal(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	item, err := s.tasks.GetCodeProposal(r.Context(), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) decideTaskCodeProposal(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		Actor     string   `json:"actor"`
		Verdict   string   `json:"verdict"`
		Rationale string   `json:"rationale"`
		Findings  []string `json:"findings"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.tasks.DecideCodeProposal(r.Context(), r.PathValue("id"),
		effectiveActor(r.Context(), input.Actor), input.Verdict, input.Rationale, input.Findings)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) applyTaskCodeProposal(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input struct {
		Actor     string                  `json:"actor"`
		Authority taskengine.RunAuthority `json:"authority"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	output, err := s.coord.Apply(r.Context(), input.Authority, r.PathValue("id"), effectiveActor(r.Context(), input.Actor))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) verifyTaskCodeProposal(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input struct {
		Actor     string                   `json:"actor"`
		Authority taskengine.RunAuthority  `json:"authority"`
		Checks    []taskcoord.CommandCheck `json:"checks"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	output, err := s.coord.Verify(r.Context(), input.Authority, r.PathValue("id"), effectiveActor(r.Context(), input.Actor), input.Checks)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) verifyFrozenTaskCodeProposal(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input struct {
		Actor     string                  `json:"actor"`
		Authority taskengine.RunAuthority `json:"authority"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	output, err := s.coord.VerifyFrozen(r.Context(), input.Authority, r.PathValue("id"), effectiveActor(r.Context(), input.Actor))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) postReviewTaskCodeProposal(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input struct {
		ProviderID string                  `json:"provider_id"`
		Authority  taskengine.RunAuthority `json:"authority"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	output, err := s.coord.Review(r.Context(), input.Authority, r.PathValue("id"), input.ProviderID)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) transitionTaskEffect(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		Action    string                  `json:"action"`
		Authority taskengine.RunAuthority `json:"authority"`
		Receipt   map[string]any          `json:"receipt"`
		Error     string                  `json:"error"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	var effect taskengine.EffectIntent
	var err error
	switch input.Action {
	case "dispatch":
		effect, err = s.tasks.DispatchEffect(r.Context(), input.Authority, r.PathValue("operation"))
	case "observe":
		effect, err = s.tasks.ObserveEffect(r.Context(), r.PathValue("operation"), input.Receipt)
	case "reconcile":
		effect, err = s.tasks.ReconcileEffect(r.Context(), r.PathValue("operation"), input.Receipt, input.Error)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be dispatch, observe or reconcile"})
		return
	}
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, effect)
}

func (s *Server) reconcileWorkspaceRunEffect(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	output, err := s.coord.ReconcileWorkspaceRun(r.Context(), r.PathValue("operation"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) reconcileTaskEffect(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	output, err := s.coord.ReconcileEffect(r.Context(), r.PathValue("operation"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) reconcileUncertainEffects(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input struct {
		Limit int `json:"limit"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	output, err := s.coord.ReconcileUncertain(r.Context(), input.Limit)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) completeTaskAttempt(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		Output    string                  `json:"output"`
		Authority taskengine.RunAuthority `json:"authority"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	attempt, err := s.tasks.CompleteAttempt(r.Context(), input.Authority, r.PathValue("id"), input.Output)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, attempt)
}

func (s *Server) reviseTaskRequirements(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input taskengine.ReviseRequirementsInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.TaskID = r.PathValue("id")
	item, err := s.tasks.ReviseRequirements(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createTaskPlan(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input taskengine.CreatePlanInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.TaskID = r.PathValue("id")
	item, err := s.tasks.CreatePlan(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) transitionTaskStep(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		Action               string `json:"action"`
		ExpectedTaskRevision int    `json:"expected_task_revision"`
		ExpectedStepRevision int    `json:"expected_step_revision"`
		Reason               string `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	var item taskengine.Task
	var err error
	switch input.Action {
	case "start":
		item, err = s.tasks.StartStep(r.Context(), r.PathValue("id"), r.PathValue("step"), input.ExpectedTaskRevision, input.ExpectedStepRevision)
	case "complete":
		item, err = s.tasks.CompleteStep(r.Context(), r.PathValue("id"), r.PathValue("step"), input.ExpectedTaskRevision, input.ExpectedStepRevision)
	case "fail":
		item, err = s.tasks.FailStep(r.Context(), r.PathValue("id"), r.PathValue("step"), input.ExpectedTaskRevision, input.ExpectedStepRevision, input.Reason)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be start, complete or fail"})
		return
	}
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) recordTaskValidation(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input taskengine.Validation
	if !decodeJSON(w, r, &input) {
		return
	}
	input.TaskID = r.PathValue("id")
	item, err := s.tasks.RecordValidation(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) createTaskCheckpoint(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input taskengine.CheckpointInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.TaskID = r.PathValue("id")
	item, err := s.tasks.Checkpoint(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) completeDurableTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		ExpectedTaskRevision int `json:"expected_task_revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.tasks.CompleteTask(r.Context(), r.PathValue("id"), input.ExpectedTaskRevision)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func taskError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, taskengine.ErrNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, taskengine.ErrStaleRevision) {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
