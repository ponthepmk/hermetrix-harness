package agentplatform

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/product"
)

var ErrBinding = errors.New("agent platform binding is unavailable")

type Binding struct {
	ID                    string
	PlatformID            string
	NodeID                string
	AgentID               string
	RepositoryID          string
	WorktreeID            string
	ProjectID             string
	OwnerPrincipalID      string
	CanonicalRoot         string
	GitCommonDir          string
	GitWorktreeDir        string
	RepositoryFingerprint string
	WorktreeFingerprint   string
	Revision              int
}

func (s *Service) RegisterBinding(ctx context.Context, repositoryID string, worktreeID *string, projectID string) (Binding, error) {
	var projectRoot, ownerID string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT root_path,owner_principal_id FROM projects WHERE id=?`, projectID).Scan(&projectRoot, &ownerID); err != nil {
		return Binding{}, fmt.Errorf("%w: project: %v", ErrBinding, err)
	}
	root, common, worktree, repositoryFingerprint, worktreeFingerprint, err := inspectRepository(projectRoot)
	if err != nil {
		return Binding{}, err
	}
	now := formatContractTime(s.trust.now())
	item := Binding{ID: identity.New("binding"), PlatformID: s.trust.PlatformID, NodeID: s.trust.NodeID,
		AgentID: s.trust.AgentID, RepositoryID: repositoryID, WorktreeID: optionalValue(worktreeID), ProjectID: projectID,
		OwnerPrincipalID: ownerID, CanonicalRoot: root, GitCommonDir: common, GitWorktreeDir: worktree,
		RepositoryFingerprint: repositoryFingerprint, WorktreeFingerprint: worktreeFingerprint, Revision: 1}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO agent_platform_bindings
		(binding_id,platform_id,node_id,agent_id,repository_id,worktree_id,project_id,owner_principal_id,canonical_root,
		 git_common_dir,git_worktree_dir,repository_fingerprint,worktree_fingerprint,binding_revision,enabled,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?)`, item.ID, item.PlatformID, item.NodeID, item.AgentID, item.RepositoryID,
		item.WorktreeID, item.ProjectID, item.OwnerPrincipalID, item.CanonicalRoot, item.GitCommonDir, item.GitWorktreeDir,
		item.RepositoryFingerprint, item.WorktreeFingerprint, item.Revision, now, now)
	if err != nil {
		return Binding{}, err
	}
	return item, nil
}

func (s *Service) resolveBinding(ctx context.Context, assignment TaskAssignment) (Binding, error) {
	var item Binding
	var currentRoot, currentOwner string
	err := s.store.DB.QueryRowContext(ctx, `SELECT b.binding_id,b.platform_id,b.node_id,b.agent_id,b.repository_id,b.worktree_id,b.project_id,
		b.owner_principal_id,b.canonical_root,b.git_common_dir,b.git_worktree_dir,b.repository_fingerprint,b.worktree_fingerprint,b.binding_revision,
		p.root_path,p.owner_principal_id
		FROM agent_platform_bindings b JOIN projects p ON p.id=b.project_id
		WHERE b.platform_id=? AND b.node_id=? AND b.agent_id=? AND b.repository_id=? AND b.worktree_id=? AND b.enabled=1`,
		s.trust.PlatformID, assignment.NodeID, assignment.AgentID, assignment.RepositoryID, optionalValue(assignment.WorktreeID)).Scan(
		&item.ID, &item.PlatformID, &item.NodeID, &item.AgentID, &item.RepositoryID, &item.WorktreeID, &item.ProjectID,
		&item.OwnerPrincipalID, &item.CanonicalRoot, &item.GitCommonDir, &item.GitWorktreeDir,
		&item.RepositoryFingerprint, &item.WorktreeFingerprint, &item.Revision, &currentRoot, &currentOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrBinding
	}
	if err != nil {
		return Binding{}, err
	}
	root, common, worktree, repositoryFingerprint, worktreeFingerprint, err := inspectRepository(currentRoot)
	if err != nil || currentOwner != item.OwnerPrincipalID || !samePath(root, item.CanonicalRoot) || !samePath(common, item.GitCommonDir) || !samePath(worktree, item.GitWorktreeDir) ||
		repositoryFingerprint != item.RepositoryFingerprint || worktreeFingerprint != item.WorktreeFingerprint {
		return Binding{}, fmt.Errorf("%w: local repository identity changed", ErrBinding)
	}
	return item, nil
}

func (s *Service) resolvePinnedBinding(ctx context.Context, bindingID string, revision int) (Binding, error) {
	var item Binding
	var currentRoot, currentOwner string
	err := s.store.DB.QueryRowContext(ctx, `SELECT b.binding_id,b.platform_id,b.node_id,b.agent_id,b.repository_id,b.worktree_id,b.project_id,
		b.owner_principal_id,b.canonical_root,b.git_common_dir,b.git_worktree_dir,b.repository_fingerprint,b.worktree_fingerprint,b.binding_revision,
		p.root_path,p.owner_principal_id
		FROM agent_platform_bindings b JOIN projects p ON p.id=b.project_id
		WHERE b.binding_id=? AND b.platform_id=? AND b.binding_revision=? AND b.enabled=1`,
		bindingID, s.trust.PlatformID, revision).Scan(&item.ID, &item.PlatformID, &item.NodeID, &item.AgentID,
		&item.RepositoryID, &item.WorktreeID, &item.ProjectID, &item.OwnerPrincipalID, &item.CanonicalRoot,
		&item.GitCommonDir, &item.GitWorktreeDir, &item.RepositoryFingerprint, &item.WorktreeFingerprint,
		&item.Revision, &currentRoot, &currentOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, ErrBinding
	}
	if err != nil {
		return Binding{}, err
	}
	root, common, worktree, repositoryFingerprint, worktreeFingerprint, inspectErr := inspectRepository(currentRoot)
	if inspectErr != nil || currentOwner != item.OwnerPrincipalID || !samePath(root, item.CanonicalRoot) ||
		!samePath(common, item.GitCommonDir) || !samePath(worktree, item.GitWorktreeDir) ||
		repositoryFingerprint != item.RepositoryFingerprint || worktreeFingerprint != item.WorktreeFingerprint {
		return Binding{}, fmt.Errorf("%w: pinned local repository identity changed", ErrBinding)
	}
	return item, nil
}

func inspectRepository(rawRoot string) (root, common, worktree, repositoryFingerprint, worktreeFingerprint string, err error) {
	root, err = product.ResolveProjectRootBinding(rawRoot)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("%w: %v", ErrBinding, err)
	}
	dotGit := filepath.Join(root, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("%w: git metadata: %v", ErrBinding, err)
	}
	if info.IsDir() {
		worktree, err = filepath.EvalSymlinks(dotGit)
		common = worktree
	} else {
		file, openErr := os.Open(dotGit)
		if openErr != nil {
			return "", "", "", "", "", fmt.Errorf("%w: git pointer", ErrBinding)
		}
		scanner := bufio.NewScanner(file)
		var line string
		if scanner.Scan() {
			line = strings.TrimSpace(scanner.Text())
		}
		closeErr := file.Close()
		if scanner.Err() != nil || closeErr != nil || !strings.HasPrefix(line, "gitdir: ") {
			return "", "", "", "", "", fmt.Errorf("%w: invalid git pointer", ErrBinding)
		}
		worktree = strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
		if !filepath.IsAbs(worktree) {
			worktree = filepath.Join(root, worktree)
		}
		worktree, err = filepath.EvalSymlinks(worktree)
		if err != nil {
			return "", "", "", "", "", fmt.Errorf("%w: git worktree metadata", ErrBinding)
		}
		common = worktree
		if data, readErr := os.ReadFile(filepath.Join(worktree, "commondir")); readErr == nil {
			common = strings.TrimSpace(string(data))
			if !filepath.IsAbs(common) {
				common = filepath.Join(worktree, common)
			}
			common, err = filepath.EvalSymlinks(common)
			if err != nil {
				return "", "", "", "", "", fmt.Errorf("%w: common git metadata", ErrBinding)
			}
		}
	}
	root, _ = filepath.Abs(root)
	common, _ = filepath.Abs(common)
	worktree, _ = filepath.Abs(worktree)
	repositoryFingerprint = fingerprint(normalizePath(common))
	worktreeFingerprint = fingerprint(normalizePath(root) + "\x00" + normalizePath(worktree))
	return root, common, worktree, repositoryFingerprint, worktreeFingerprint, nil
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func normalizePath(value string) string {
	value = filepath.Clean(value)
	if runtime.GOOS == "windows" {
		value = strings.ToLower(value)
	}
	return value
}

func samePath(left, right string) bool { return normalizePath(left) == normalizePath(right) }

func optionalValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func formatContractTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}
