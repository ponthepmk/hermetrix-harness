package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hermetrix-harness/internal/agentplatform"
	"hermetrix-harness/internal/agentplatform/fixture"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

type workspaceReceipt struct {
	Digest string `json:"digest"`
	Files  int    `json:"files"`
	Links  int    `json:"links"`
	Bytes  int64  `json:"bytes"`
	Scope  string `json:"scope"`
}

type demoReceipt struct {
	Transport                     string                           `json:"transport"`
	Receiver                      string                           `json:"receiver"`
	RealPiContacted               bool                             `json:"real_pi_contacted"`
	CandidateExecuted             bool                             `json:"candidate_executed"`
	FixtureRoot                   string                           `json:"fixture_root"`
	SchemaVersion                 int                              `json:"schema_version"`
	DurableTaskProjections        int                              `json:"durable_task_projections"`
	PlatformTaskID                string                           `json:"platform_task_id"`
	HarnessTaskID                 string                           `json:"harness_task_id"`
	IDsAreSeparate                bool                             `json:"ids_are_separate"`
	TaskRuns                      int                              `json:"task_runs"`
	Attempts                      int                              `json:"attempts"`
	Effects                       int                              `json:"effects"`
	BackgroundJobs                int                              `json:"background_jobs"`
	ProjectionArtifacts           int                              `json:"projection_artifacts"`
	RunUpdates                    int                              `json:"durable_run_updates"`
	ACKHighestContiguous          int64                            `json:"ack_highest_contiguous"`
	Candidate                     string                           `json:"recorded_candidate"`
	DuplicateAssignmentIdempotent bool                             `json:"duplicate_assignment_idempotent"`
	DigestConflictRejected        bool                             `json:"digest_conflict_rejected"`
	ReplayExactBytes              bool                             `json:"replay_exact_bytes"`
	ReplayDecisionCountStable     bool                             `json:"replay_decision_count_stable"`
	ReceiverRestartRecovered      bool                             `json:"receiver_restart_recovered"`
	SenderRestartRecovered        bool                             `json:"sender_restart_recovered"`
	OutOfOrderACKs                []int64                          `json:"out_of_order_acks"`
	InvocationCounters            agentplatform.InvocationCounters `json:"invocation_counters"`
	WorkspaceBefore               workspaceReceipt                 `json:"workspace_before"`
	WorkspaceAfter                workspaceReceipt                 `json:"workspace_after"`
	WorkspaceIdentical            bool                             `json:"workspace_identical"`
}

type loseACKOnce struct {
	receiver *fixture.Receiver
	lost     bool
}

func (l *loseACKOnce) Send(ctx context.Context, raw []byte) ([]byte, error) {
	ack, err := l.receiver.Send(ctx, raw)
	if err != nil {
		return nil, err
	}
	if !l.lost {
		l.lost = true
		return nil, errors.New("simulated ACK loss after durable receive")
	}
	return ack, nil
}

func main() {
	if len(os.Args) != 3 || os.Args[1] != "demo" || os.Args[2] != "--fixture-only" {
		fatalf("usage: hermetrix-h1 demo --fixture-only")
	}
	if err := runDemo(context.Background()); err != nil {
		fatalf("H1 demo failed: %v", err)
	}
}

func runDemo(ctx context.Context) error {
	workspace, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	before, err := snapshotWorkspace(workspace)
	if err != nil {
		return err
	}
	fixtureRoot, err := prepareFixtureRoot(workspace)
	if err != nil {
		return err
	}
	repositoryRoot := filepath.Join(fixtureRoot, "repository")
	if err = os.MkdirAll(filepath.Join(repositoryRoot, ".git"), 0o700); err != nil {
		return err
	}
	dataStore, err := store.Open(ctx, filepath.Join(fixtureRoot, "sender-data"))
	if err != nil {
		return err
	}
	defer dataStore.Close()
	projectID, err := createFixtureProject(ctx, dataStore, repositoryRoot)
	if err != nil {
		return err
	}
	senderTrust, err := makeTrust("fixture-sender", "fixture-agent")
	if err != nil {
		return err
	}
	taskService := taskengine.NewService(dataStore)
	service, err := agentplatform.New(dataStore, taskService, senderTrust)
	if err != nil {
		return err
	}
	if _, err = service.RegisterBinding(ctx, "fixture-repository", nil, projectID); err != nil {
		return err
	}
	assignment, raw, err := signedAssignment(senderTrust)
	if err != nil {
		return err
	}
	first, err := service.Intake(ctx, raw)
	if err != nil {
		return err
	}
	second, err := service.Intake(ctx, raw)
	if err != nil {
		return err
	}
	duplicateOK := first.HarnessTaskID == second.HarnessTaskID
	changed := assignment
	changed.Title = "immutable digest conflict"
	conflictRaw, _, err := agentplatform.SignEnvelope("TaskAssignment", agentplatform.TaskAssignmentRevision,
		senderTrust.NodeID, "fixture-assignment:conflict", changed, senderTrust.KeyID,
		ed25519.PrivateKey(senderTrust.PrivateKey))
	if err != nil {
		return err
	}
	_, conflictErr := service.Intake(ctx, conflictRaw)
	conflictRejected := errors.Is(conflictErr, agentplatform.ErrDigestConflict)
	observation, err := service.ObserveInitial(ctx, first.AssignmentID)
	if err != nil {
		return err
	}
	decisionCount := service.DecisionInvocationCount()
	replayedObservation, err := service.ObserveInitial(ctx, first.AssignmentID)
	if err != nil {
		return err
	}
	replayExact := bytes.Equal(observation.EnvelopeBytes, replayedObservation.EnvelopeBytes)
	replayStable := decisionCount == service.DecisionInvocationCount()
	receiverTrust, err := makeTrust("fixture-receiver", "fixture-receiver-agent")
	if err != nil {
		return err
	}
	receiverRoot := filepath.Join(fixtureRoot, "receiver-data")
	receiver, err := fixture.OpenReceiver(ctx, receiverRoot, senderTrust, receiverTrust)
	if err != nil {
		return err
	}
	lossy := &loseACKOnce{receiver: receiver}
	if _, err = service.DeliverPending(ctx, receiverTrust, lossy, 3); err != nil {
		receiver.Close()
		return err
	}
	if err = receiver.Close(); err != nil {
		return err
	}
	receiver, err = fixture.OpenReceiver(ctx, receiverRoot, senderTrust, receiverTrust)
	if err != nil {
		return err
	}
	defer receiver.Close()
	restartedSender, err := agentplatform.New(dataStore, taskengine.NewService(dataStore), senderTrust)
	if err != nil {
		return err
	}
	delivery, err := restartedSender.DeliverPending(ctx, receiverTrust, receiver, 3)
	if err != nil {
		return err
	}
	ack, err := receiver.AckedSequence(ctx, first.PlatformRunID)
	if err != nil {
		return err
	}
	duplicateAckRaw, err := receiver.Send(ctx, observation.EnvelopeBytes)
	if err != nil {
		return err
	}
	_, duplicateAck, err := agentplatform.ValidateACK(duplicateAckRaw, receiverTrust)
	if err != nil || duplicateAck.AckedSequence != ack {
		return fmt.Errorf("duplicate delivery changed ACK: %v", err)
	}
	orderACKs, err := demonstrateOutOfOrder(ctx, receiver, senderTrust, receiverTrust)
	if err != nil {
		return err
	}
	version, err := dataStore.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	receipt := demoReceipt{Transport: "fixture", Receiver: "local", RealPiContacted: false, CandidateExecuted: false,
		FixtureRoot: fixtureRoot, SchemaVersion: version, PlatformTaskID: first.PlatformTaskID, HarnessTaskID: first.HarnessTaskID,
		IDsAreSeparate: first.PlatformTaskID != first.HarnessTaskID, ACKHighestContiguous: ack,
		Candidate: observation.DecisionActionID, DuplicateAssignmentIdempotent: duplicateOK,
		DigestConflictRejected: conflictRejected, ReplayExactBytes: replayExact,
		ReplayDecisionCountStable: replayStable, ReceiverRestartRecovered: ack == 1,
		SenderRestartRecovered: delivery.Pending == 0 && delivery.Acked == 1, OutOfOrderACKs: orderACKs,
		InvocationCounters: service.InvocationCounters(), WorkspaceBefore: before}
	for table, target := range map[string]*int{
		"durable_tasks": &receipt.DurableTaskProjections, "task_runs": &receipt.TaskRuns,
		"task_step_attempts": &receipt.Attempts, "task_effect_intents": &receipt.Effects,
		"background_jobs": &receipt.BackgroundJobs, "agent_platform_outbox": &receipt.RunUpdates,
	} {
		if err = dataStore.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(target); err != nil {
			return err
		}
	}
	if err = dataStore.DB.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE kind='agent_platform_projection'`).Scan(&receipt.ProjectionArtifacts); err != nil {
		return err
	}
	after, err := snapshotWorkspace(workspace)
	if err != nil {
		return err
	}
	receipt.WorkspaceAfter = after
	receipt.WorkspaceIdentical = before == after
	if !receipt.IDsAreSeparate || receipt.DurableTaskProjections != 1 || receipt.TaskRuns != 0 || receipt.Attempts != 0 ||
		receipt.Effects != 0 || receipt.BackgroundJobs != 0 || receipt.ProjectionArtifacts != 1 || receipt.RunUpdates != 1 ||
		receipt.ACKHighestContiguous != 1 || !receipt.DuplicateAssignmentIdempotent || !receipt.DigestConflictRejected ||
		!receipt.ReplayExactBytes || !receipt.ReplayDecisionCountStable || !receipt.WorkspaceIdentical ||
		receipt.InvocationCounters.PlanEffect != 0 || receipt.InvocationCounters.Command != 0 ||
		receipt.InvocationCounters.Provider != 0 || receipt.InvocationCounters.Tool != 0 ||
		receipt.InvocationCounters.WorkspaceWrite != 0 {
		return fmt.Errorf("acceptance invariant failed: %+v", receipt)
	}
	encoded, _ := json.MarshalIndent(receipt, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func prepareFixtureRoot(workspace string) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("HERMETRIX_H1_FIXTURE_ROOT")); configured != "" {
		root, err := filepath.Abs(configured)
		if err != nil {
			return "", err
		}
		if !outside(root, workspace) {
			return "", fmt.Errorf("fixture root must be outside target workspace")
		}
		if info, err := os.Stat(filepath.Join(root, ".hermetrix-h1-fixture")); err != nil || info.IsDir() {
			return "", fmt.Errorf("configured fixture root requires .hermetrix-h1-fixture marker")
		}
		if _, err := os.Stat(filepath.Join(root, "sender-data", "hermetrix.db")); err == nil {
			return "", fmt.Errorf("fixture root is not fresh")
		}
		return root, nil
	}
	root, err := os.MkdirTemp("", "hermetrix-h1-fixture-")
	if err != nil {
		return "", err
	}
	if !outside(root, workspace) {
		return "", fmt.Errorf("temporary fixture root is inside target workspace")
	}
	if err = os.WriteFile(filepath.Join(root, ".hermetrix-h1-fixture"), []byte("fixture-only\n"), 0o600); err != nil {
		return "", err
	}
	return root, nil
}

func outside(path, parent string) bool {
	if !strings.EqualFold(filepath.VolumeName(path), filepath.VolumeName(parent)) {
		return true
	}
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != "." && (relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func createFixtureProject(ctx context.Context, dataStore *store.Store, root string) (string, error) {
	canonical, err := product.ResolveProjectRootBinding(root)
	if err != nil {
		return "", err
	}
	owner, err := dataStore.LocalPrincipalID(ctx)
	if err != nil {
		return "", err
	}
	id := identity.New("project")
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO projects(id,name,root_path,state,created_at,updated_at,owner_principal_id)
		VALUES(?,'H1 fixture',?,'active',?,?,?)`, id, canonical, now, now, owner)
	return id, err
}

func makeTrust(node, agent string) (agentplatform.Trust, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return agentplatform.Trust{}, err
	}
	return agentplatform.Trust{PlatformID: "fixture-platform", NodeID: node, AgentID: agent,
		KeyID: node + "-key", PublicKey: public, PrivateKey: private}, nil
}

func signedAssignment(trust agentplatform.Trust) (agentplatform.TaskAssignment, []byte, error) {
	now := time.Now().UTC()
	scope := agentplatform.AccessScope{Access: "read", RepositoryID: "fixture-repository"}
	claim := agentplatform.AssignmentAuthorizationClaim{AssignmentID: "fixture-assignment", AssignmentRevision: 1,
		NodeID: trust.NodeID, AgentID: trust.AgentID, PlatformRunID: "fixture-platform-run",
		RepositoryID: scope.RepositoryID, AccessScope: scope, IssuedAt: now.Add(-time.Minute).Format("2006-01-02T15:04:05.000Z"),
		ExpiresAt: now.Add(10 * time.Minute).Format("2006-01-02T15:04:05.000Z"),
		Integrity: agentplatform.ClaimIntegrity{Alg: "ed25519"}}
	claim, err := agentplatform.SignClaim(claim, ed25519.PrivateKey(trust.PrivateKey))
	if err != nil {
		return agentplatform.TaskAssignment{}, nil, err
	}
	assignment := agentplatform.TaskAssignment{ContractVersion: agentplatform.ContractVersion,
		SchemaRevision: agentplatform.TaskAssignmentRevision, AssignmentID: claim.AssignmentID, AssignmentRevision: 1,
		PlatformRunID: claim.PlatformRunID, AgentID: trust.AgentID, NodeID: trust.NodeID,
		RepositoryID: scope.RepositoryID, TaskID: "fixture-platform-task", Title: "H1 read-only proof",
		OriginalRequest: "Record a safe recommendation without execution", Goal: "Prove fixture-only managed integration",
		AcceptanceCriteria: []agentplatform.AcceptanceCriterion{{ID: "AC-1", Description: "No candidate executes"}},
		AccessScope:        scope, AuthorizationClaim: claim, IssuedAt: now.Format("2006-01-02T15:04:05.000Z")}
	raw, _, err := agentplatform.SignEnvelope("TaskAssignment", agentplatform.TaskAssignmentRevision, trust.NodeID,
		"fixture-assignment:1", assignment, trust.KeyID, ed25519.PrivateKey(trust.PrivateKey))
	return assignment, raw, err
}

func demonstrateOutOfOrder(ctx context.Context, receiver *fixture.Receiver, sender, receiverTrust agentplatform.Trust) ([]int64, error) {
	acks := make([]int64, 0, 3)
	for _, sequence := range []int64{1, 3, 2} {
		now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		update := agentplatform.RunUpdate{PlatformRunID: "fixture-ordering-run", Sequence: sequence,
			ExecutionStatus: "accepted", Progress: agentplatform.ProgressFacts{Phase: "initializing", BlockedReasons: []string{}},
			ProjectionRef: agentplatform.EvidenceRef{EvidenceID: "fixture-order-artifact", Type: "artifact",
				OriginNodeID: sender.NodeID, ContentDigest: "sha256:" + strings.Repeat("a", 64), CreatedAt: now}, CreatedAt: now}
		raw, _, err := agentplatform.SignEnvelope("RunUpdate", agentplatform.RunUpdateRevision, sender.NodeID,
			fmt.Sprintf("fixture-order:%d", sequence), update, sender.KeyID, ed25519.PrivateKey(sender.PrivateKey))
		if err != nil {
			return nil, err
		}
		ackRaw, err := receiver.Send(ctx, raw)
		if err != nil {
			return nil, err
		}
		_, ack, err := agentplatform.ValidateACK(ackRaw, receiverTrust)
		if err != nil {
			return nil, err
		}
		acks = append(acks, ack.AckedSequence)
	}
	return acks, nil
}

func snapshotWorkspace(root string) (workspaceReceipt, error) {
	entries := []string{}
	receipt := workspaceReceipt{Scope: "all files except .git/.cache/.hermetrix/node_modules runtime trees"}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == ".cache" || base == ".hermetrix" || base == "node_modules" {
				return filepath.SkipDir
			}
		}
		if relative == "." || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			receipt.Links++
			entries = append(entries, filepath.ToSlash(relative)+"\x00link\x00"+target)
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		receipt.Files++
		receipt.Bytes += int64(len(body))
		entries = append(entries, filepath.ToSlash(relative)+"\x00"+info.Mode().String()+"\x00"+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		return receipt, err
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	receipt.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return receipt, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
