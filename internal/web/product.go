package web

import (
	"encoding/base64"
	"errors"
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

func (s *Server) updateVisibility(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.VisibilityInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Actor = effectiveActor(r.Context(), input.Actor)
	receipt, err := s.product.UpdateVisibility(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (s *Server) updateArtifactVisibility(w http.ResponseWriter, r *http.Request) {
	s.updateBoundVisibility(w, r, "artifact")
}
func (s *Server) updateTaskVisibility(w http.ResponseWriter, r *http.Request) {
	s.updateBoundVisibility(w, r, "task")
}
func (s *Server) updateMemoryVisibility(w http.ResponseWriter, r *http.Request) {
	s.updateBoundVisibility(w, r, "memory")
}
func (s *Server) updateSkillVisibility(w http.ResponseWriter, r *http.Request) {
	s.updateBoundVisibility(w, r, "skill")
}
func (s *Server) updateBoundVisibility(w http.ResponseWriter, r *http.Request, kind string) {
	if !s.requireProduct(w) {
		return
	}
	var input product.VisibilityInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.ObjectKind = kind
	input.ObjectID = r.PathValue("id")
	input.Actor = effectiveActor(r.Context(), input.Actor)
	receipt, err := s.product.UpdateVisibility(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
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

// pinProject and openProject are the two things the picker writes. Nothing else
// about a project changes from that screen.
func (s *Server) pinProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Pinned bool `json:"pinned"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.PinProject(r.Context(), r.PathValue("id"), input.Pinned)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) openProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.MarkProjectOpened(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
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
		var conflict *product.PreimageChangedError
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{
				"code": "preimage_changed", "message": conflict.Error(), "path": conflict.Path,
				"current_sha256": conflict.CurrentSHA256,
			}})
			return
		}
		var committed *product.MutationCommittedReceiptFailed
		if errors.As(err, &committed) {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{
				"code": "mutation_committed_receipt_failed", "message": committed.Error(),
				"receipt_error_code": committed.ReceiptErrorCode,
			}, "result": committed.Result})
			return
		}
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

func (s *Server) listTerminals(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListTerminals(r.Context(), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) startTerminal(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.StartTerminalInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.StartTerminal(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) terminalOutput(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	item, err := s.product.TerminalOutput(r.Context(), r.PathValue("id"), cursor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) writeTerminal(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Input string `json:"input"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.product.WriteTerminal(r.Context(), r.PathValue("id"), input.Input); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) resizeTerminal(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Columns uint16 `json:"columns"`
		Rows    uint16 `json:"rows"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.product.ResizeTerminal(r.Context(), r.PathValue("id"), input.Columns, input.Rows); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) closeTerminal(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.CloseTerminal(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listBrowserTabs(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListBrowserTabs(r.Context(), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) openBrowserTab(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.OpenBrowserTabInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.OpenBrowserTab(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) browserAction(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.BrowserActionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.BrowserAction(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) listAgentTeams(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListAgentTeams(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveAgentTeam(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.AgentTeamInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.SaveAgentTeam(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listTeamRuns(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.product.ListTeamRuns(r.Context(), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) startTeamRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.StartTeamRunInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.StartTeamRun(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) getTeamRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.GetTeamRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) cancelTeamRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.CancelTeamRun(r.Context(), r.PathValue("id"), input.Actor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) decideTeamTaskApproval(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.TeamApprovalDecisionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.DecideTeamTaskApproval(r.Context(), r.PathValue("id"), r.PathValue("task"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
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

// uploadImageArtifact preserves the legacy JSON/base64 request shape while
// delegating decoded-content validation to the media boundary.
func (s *Server) uploadImageArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		ProjectID string `json:"project_id"`
		SessionID string `json:"session_id"`
		Name      string `json:"name"`
		MIMEType  string `json:"mime_type"`
		Base64    string `json:"base64"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	mime := strings.ToLower(strings.TrimSpace(input.MIMEType))
	extensions := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}
	extension, ok := extensions[mime]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "only png, jpeg or webp images are accepted"})
		return
	}
	raw, err := base64.StdEncoding.DecodeString(input.Base64)
	if err != nil || len(raw) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "image body is not valid base64"})
		return
	}
	if len(raw) > 8<<20 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "image exceeds the 8 MiB composer limit"})
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "pasted-image"
	}
	if !strings.Contains(name, ".") {
		name += "." + extension
	}
	item, err := s.product.UploadMedia(r.Context(), product.MediaUploadInput{ProjectID: input.ProjectID,
		SessionID: input.SessionID, Name: name, MIMEType: mime, Data: raw})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) createDeliverable(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.DeliverableInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.CreateDeliverable(r.Context(), input)
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

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (8<<20)+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "media upload exceeds the image upload limit"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "multipart field file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "image exceeds 8 MiB"})
		return
	}
	item, err := s.product.UploadMedia(r.Context(), product.MediaUploadInput{ProjectID: r.FormValue("project_id"),
		SessionID: r.FormValue("session_id"), Name: header.Filename, Data: data})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) startMediaJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.MediaJobInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.StartMediaJob(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) getMediaJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.GetMediaJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) cancelMediaJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.CancelMediaJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createSharePreview(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.SharePreviewInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.ProjectID, input.Actor = r.PathValue("id"), effectiveActor(r.Context(), input.Actor)
	item, err := s.product.CreateSharePreview(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) createShareExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.ShareExportInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.ProjectID = r.PathValue("id")
	item, err := s.product.ExportShare(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) getShareExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.GetShareExport(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) getShareExportContent(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, data, err := s.product.ShareExportContent(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.hermetrix.project-share+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, item.ID+".hermetrix-share.zip"))
	w.Header().Set("X-Content-Checksum", item.PackageChecksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) previewShareImport(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (512<<20)+1)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read share package: " + err.Error()})
		return
	}
	item, err := s.product.PreviewShareImport(r.Context(), data, effectiveActor(r.Context(), r.URL.Query().Get("actor")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) applyShareImport(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.ApplyShareImportInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Actor = effectiveActor(r.Context(), input.Actor)
	item, err := s.product.ApplyShareImport(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) exportWorkspaceMigration(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.WorkspaceExportInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Actor = effectiveActor(r.Context(), input.Actor)
	item, _, err := s.product.ExportWorkspace(r.Context(), input)
	input.Passphrase = ""
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getWorkspaceMigration(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.GetWorkspaceMigration(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) getWorkspaceMigrationContent(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, data, err := s.product.WorkspaceMigrationContent(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.hermetrix.workspace+age")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, item.ID+".age"))
	w.Header().Set("X-Content-Checksum", item.PackageChecksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) previewWorkspaceMigration(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (256<<20)+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid workspace migration multipart body"})
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "multipart field file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (256<<20)+1))
	if err != nil || len(data) > 256<<20 {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "workspace package exceeds 256 MiB"})
		return
	}
	passphrase := r.FormValue("passphrase")
	item, err := s.product.PreviewWorkspaceImport(r.Context(), product.WorkspaceImportPreviewInput{EncryptedPackage: data, Passphrase: passphrase, Actor: effectiveActor(r.Context(), r.FormValue("actor"))})
	passphrase = ""
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) applyWorkspaceMigration(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.WorkspaceImportApplyInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Actor = effectiveActor(r.Context(), input.Actor)
	item, err := s.product.ApplyWorkspaceImport(r.Context(), input)
	input.Passphrase = ""
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) createFullRecovery(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Actor string `json:"actor"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, _, err := s.product.CreateFullRecovery(r.Context(), effectiveActor(r.Context(), input.Actor))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getFullRecoveryContent(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, data, err := s.product.BackupData(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if item.Kind != "full_recovery" || item.State != "completed" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "recovery package is not completed"})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.hermetrix.full-recovery+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, item.ID+".hermetrix-recovery.zip"))
	w.Header().Set("X-Content-Checksum", item.Checksum)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) verifyFullRecovery(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (512<<20)+1)
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) > 512<<20 {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "recovery package exceeds 512 MiB"})
		return
	}
	report, err := product.VerifyFullRecovery(data)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
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
	item, err := s.product.PreviewImport(r.Context(), data, effectiveActor(r.Context(), r.URL.Query().Get("actor")))
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
