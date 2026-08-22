package web

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"hermetrix-harness/internal/curator"
	"hermetrix-harness/internal/product"
)

func (s *Server) requireProduct(w http.ResponseWriter) bool {
	if s.product == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "product workspace service is unavailable"})
		return false
	}
	return true
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListProjects(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.ProjectInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.SaveProject(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) browseProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.BrowseProject(r.Context(), r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) readProjectFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.ReadProjectFile(r.Context(), r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) writeProjectFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.WriteFileInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.WriteProjectFile(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) startProjectCommand(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.CommandInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.ProjectID = r.PathValue("id")
	item, err := s.product.StartCommand(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListJobs(r.Context(), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.CancelJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListArtifacts(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.ArtifactInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.CreateArtifact(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getArtifactContent(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, data, err := s.product.GetArtifact(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", item.MIMEType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename=%q`, strings.ReplaceAll(item.Name, `"`, "")))
	w.Header().Set("X-Content-Checksum", item.Checksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) listSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveSetting(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.SaveSetting(r.Context(), input.Key, input.Value)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listMemories(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListMemories(r.Context(), r.URL.Query().Get("scope_kind"), r.URL.Query().Get("scope_ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveMemory(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.MemoryInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.SaveMemory(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) archiveMemory(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	if err := s.product.ArchiveMemory(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) usageSummary(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.Usage(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListBackups(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, _, err := s.product.ExportBackup(r.Context(), input.Actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, data, err := s.product.BackupData(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.hermetrix.backup+json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="hermetrix-%s.json"`, item.ID))
	w.Header().Set("X-Content-Checksum", item.Checksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) previewImport(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read import: " + err.Error()})
		return
	}
	item, err := s.product.PreviewImport(r.Context(), data, r.URL.Query().Get("actor"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) applyImport(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.ApplyImport(r.Context(), r.PathValue("id"), input.Actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listCuratorFindings(w http.ResponseWriter, r *http.Request) {
	items, err := s.curator.ListFindings(r.Context(), r.URL.Query().Get("run_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := s.curator.ListSchedules(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveSchedule(w http.ResponseWriter, r *http.Request) {
	var input curator.ScheduleInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.curator.SaveSchedule(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) runDueMaintenance(w http.ResponseWriter, r *http.Request) {
	var input curator.SystemState
	if !decodeJSON(w, r, &input) {
		return
	}
	items, err := s.curator.RunDue(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) systemState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, curator.DetectSystemState(r.Context()))
}

func (s *Server) listGCRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.curator.ListGCRuns(r.Context(), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) dryRunGC(w http.ResponseWriter, r *http.Request) {
	item, err := s.curator.DryRunGC(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) applyGC(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.curator.ApplyGC(r.Context(), r.PathValue("id"), input.Actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) restoreGC(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.curator.RestoreGC(r.Context(), r.PathValue("id"), input.Actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
