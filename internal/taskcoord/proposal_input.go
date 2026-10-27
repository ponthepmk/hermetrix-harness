package taskcoord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

const maxInputBodyBytes = 16 << 20

func (s *Service) loadProposalInput(ctx context.Context, proposal taskengine.CodeProposal, proposalArtifact product.Artifact, bundle proposalBundle) (worker.Task, error) {
	if s == nil || s.products == nil {
		return worker.Task{}, errors.New("service and products must be nonnil")
	}
	if bundle.Version != 2 {
		return worker.Task{}, fmt.Errorf("proposal bundle version must be 2, got %d", bundle.Version)
	}
	if strings.TrimSpace(bundle.InputArtifactID) == "" {
		return worker.Task{}, errors.New("input_artifact_id must be nonblank")
	}
	if strings.TrimSpace(proposal.ID) == "" || strings.TrimSpace(proposal.TaskID) == "" || strings.TrimSpace(proposal.StepID) == "" || strings.TrimSpace(proposal.AttemptID) == "" || strings.TrimSpace(proposal.ProjectID) == "" || strings.TrimSpace(proposal.PacketHash) == "" {
		return worker.Task{}, errors.New("proposal id, task_id, step_id, attempt_id, project_id and packet_hash must be nonblank")
	}
	if proposalArtifact.ProjectID != proposal.ProjectID || proposalArtifact.Kind != "code_proposal" {
		return worker.Task{}, fmt.Errorf("proposal artifact project/kind mismatch: project=%q kind=%q", proposalArtifact.ProjectID, proposalArtifact.Kind)
	}
	projectRoot, ok := proposalArtifact.Metadata["project_root"].(string)
	if !ok || strings.TrimSpace(projectRoot) == "" {
		return worker.Task{}, errors.New("proposal artifact metadata project_root must be a nonempty string")
	}

	inputArtifact, inputBody, err := s.products.GetArtifact(ctx, bundle.InputArtifactID)
	if err != nil {
		return worker.Task{}, fmt.Errorf("load input artifact: %w", err)
	}
	if inputArtifact.Kind != "code_proposal_input" {
		return worker.Task{}, fmt.Errorf("input artifact kind must be code_proposal_input, got %q", inputArtifact.Kind)
	}
	if inputArtifact.ProjectID != proposal.ProjectID {
		return worker.Task{}, fmt.Errorf("input artifact project_id must match proposal, got %q", inputArtifact.ProjectID)
	}
	if len(inputBody) == 0 || len(inputBody) > maxInputBodyBytes {
		return worker.Task{}, fmt.Errorf("input body must be nonempty and at most 16MiB, got %d", len(inputBody))
	}
	inputSHA256 := worker.Hash(string(inputBody))
	if inputSHA256 != inputArtifact.Checksum {
		return worker.Task{}, fmt.Errorf("input checksum mismatch: artifact=%q computed=%q", inputArtifact.Checksum, inputSHA256)
	}
	if inputSHA256 != bundle.Result.InputSHA256 {
		return worker.Task{}, fmt.Errorf("input sha256 mismatch: bundle=%q computed=%q", bundle.Result.InputSHA256, inputSHA256)
	}

	if inputArtifact.Metadata == nil {
		return worker.Task{}, errors.New("input artifact metadata must be present")
	}
	if taskID, ok := inputArtifact.Metadata["task_id"].(string); !ok || taskID != proposal.TaskID {
		return worker.Task{}, fmt.Errorf("input metadata task_id must equal proposal task_id")
	}
	if stepID, ok := inputArtifact.Metadata["step_id"].(string); !ok || stepID != proposal.StepID {
		return worker.Task{}, fmt.Errorf("input metadata step_id must equal proposal step_id")
	}
	if attemptID, ok := inputArtifact.Metadata["attempt_id"].(string); !ok || attemptID != proposal.AttemptID {
		return worker.Task{}, fmt.Errorf("input metadata attempt_id must equal proposal attempt_id")
	}
	if packetHash, ok := inputArtifact.Metadata["packet_hash"].(string); !ok || packetHash != proposal.PacketHash {
		return worker.Task{}, fmt.Errorf("input metadata packet_hash must equal proposal packet_hash")
	}
	if proposalID, ok := inputArtifact.Metadata["proposal_id"].(string); !ok || proposalID != proposal.ID {
		return worker.Task{}, fmt.Errorf("input metadata proposal_id must equal proposal id")
	}
	if root, ok := inputArtifact.Metadata["project_root"].(string); !ok || root != projectRoot {
		return worker.Task{}, fmt.Errorf("input metadata project_root must equal proposal artifact project_root")
	}

	var task worker.Task
	dec := json.NewDecoder(bytes.NewReader(inputBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&task); err != nil {
		return worker.Task{}, fmt.Errorf("decode input task: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return worker.Task{}, fmt.Errorf("input body must contain exactly one JSON value: %w", err)
	}

	frozen, _, _, err := worker.FreezeTask(task)
	if err != nil {
		return worker.Task{}, fmt.Errorf("freeze task: %w", err)
	}
	if !strings.HasPrefix(frozen.ID, proposal.TaskID+":") || !strings.HasSuffix(frozen.ID, ":"+proposal.PacketHash) || frozen.ID != bundle.Result.TaskID {
		return worker.Task{}, fmt.Errorf("frozen task id must bind proposal task/packet and bundle result task id")
	}
	if err := worker.ValidateResult(frozen, bundle.Result); err != nil {
		return worker.Task{}, fmt.Errorf("validate result: %w", err)
	}
	return frozen, nil
}
