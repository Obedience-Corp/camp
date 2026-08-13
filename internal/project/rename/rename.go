package rename

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

type fileBackup struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

type transaction struct {
	plan           *PlanResult
	journal        string
	backups        []fileBackup
	movedWorktrees []WorktreeChange
	structureMoved bool
	commonOld      string
	commonNew      string
	commonMoved    bool
	oldRemote      string
	remoteUpdated  bool
	snapshotsMoved bool
}

type journalRecord struct {
	Schema      string `json:"schema"`
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	UpdatedAt   string `json:"updated_at"`
	Error       string `json:"error,omitempty"`
}

// Apply executes a previously validated plan, verifies the new identity, and
// rolls completed steps back in reverse order if any required step fails.
func Apply(ctx context.Context, plan *PlanResult, opts Options) (*Result, error) {
	if plan == nil {
		return nil, camperrors.NewValidation("plan", "must not be nil", nil)
	}
	if opts.DryRun {
		return &Result{Plan: plan}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	refreshed, err := Plan(ctx, plan.CampaignRoot, plan.OldPath, plan.NewName, opts)
	if err != nil {
		return nil, err
	}
	refreshed.OperationID = plan.OperationID
	plan = refreshed
	result := &Result{Plan: plan}
	tx := &transaction{plan: plan}
	if err := tx.begin(ctx); err != nil {
		return result, err
	}
	result.RecoveryJournal = tx.journal

	fail := func(operationErr error) (*Result, error) {
		rollbackErr := tx.rollback(context.WithoutCancel(ctx))
		result.RolledBack = rollbackErr == nil
		if rollbackErr != nil {
			_ = tx.writeJournal("recovery-required", errors.Join(operationErr, rollbackErr))
			return result, camperrors.WrapJoin(operationErr, rollbackErr,
				"project rename failed and rollback was incomplete; recovery journal: "+tx.journal)
		}
		_ = os.Remove(tx.journal)
		return result, operationErr
	}

	for _, change := range plan.Worktrees {
		if !change.Moved {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(change.After), 0o755); err != nil {
			return fail(camperrors.Wrap(err, "create worktree destination"))
		}
		repo := tx.repositoryPathBefore()
		if err := worktree.NewGitWorktree(repo).Move(ctx, change.Before, change.After); err != nil {
			return fail(camperrors.Wrap(err, "move project worktree"))
		}
		tx.movedWorktrees = append(tx.movedWorktrees, change)
		result.Steps = append(result.Steps, "moved worktree "+change.Before+" -> "+change.After)
	}

	if err := tx.moveStructure(ctx); err != nil {
		return fail(err)
	}
	result.Steps = append(result.Steps, "renamed "+plan.OldPath+" -> "+plan.NewPath)

	if plan.Kind == KindSubmodule && plan.Initialized {
		if err := tx.migrateSubmoduleGitDir(ctx); err != nil {
			return fail(err)
		}
		result.Steps = append(result.Steps, "migrated submodule Git directory")
	}

	changedMetadata, err := applyMetadata(plan)
	if err != nil {
		return fail(camperrors.Wrap(err, "migrate Camp project references"))
	}
	for _, path := range changedMetadata {
		result.Steps = append(result.Steps, "updated "+path)
	}
	if containsSnapshotChange(plan.Metadata) {
		tx.snapshotsMoved = true
	}

	if opts.RemoteURL != "" {
		if err := tx.updateRemote(ctx, opts.RemoteURL); err != nil {
			return fail(err)
		}
		result.Steps = append(result.Steps, "updated origin remote")
	}

	if err := verify(ctx, plan); err != nil {
		return fail(err)
	}
	result.Verified = true
	result.ResidualReferences = residualReferences(ctx, plan)
	if opts.RemoteURL != "" && opts.VerifyRemote && plan.Initialized {
		if err := git.VerifyRemote(ctx, tx.repositoryPathAfter(), "origin"); err != nil {
			result.Warnings = append(result.Warnings, "remote connectivity check failed: "+err.Error())
		}
	}

	if err := tx.writeJournal("verified", nil); err != nil {
		result.Warnings = append(result.Warnings, "could not finalize transaction journal: "+err.Error())
	} else {
		_ = os.Remove(tx.journal)
		result.RecoveryJournal = ""
	}
	return result, nil
}

func (tx *transaction) begin(ctx context.Context) error {
	gitDir, err := git.RunGitCmd(ctx, tx.plan.CampaignRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	tx.journal = filepath.Join(strings.TrimSpace(gitDir), "camp", "transactions", "project-rename-"+tx.plan.OperationID+".json")
	if err := os.MkdirAll(filepath.Dir(tx.journal), 0o755); err != nil {
		return err
	}

	files, err := metadataFiles(tx.plan.CampaignRoot, tx.plan.OldName)
	if err != nil {
		return err
	}
	rootGitDir := strings.TrimSpace(gitDir)
	paths := []string{
		filepath.Join(tx.plan.CampaignRoot, ".gitmodules"),
		filepath.Join(rootGitDir, "config"),
		filepath.Join(rootGitDir, "index"),
	}
	for _, file := range files {
		paths = append(paths, file.abs)
	}
	for _, path := range uniqueStrings(paths) {
		backup := fileBackup{path: path}
		if info, err := os.Stat(path); err == nil {
			backup.exists = true
			backup.mode = info.Mode().Perm()
			backup.data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		tx.backups = append(tx.backups, backup)
	}
	if tx.plan.Initialized {
		tx.oldRemote, _ = git.RemoteOriginURL(ctx, tx.repositoryPathBefore())
	}
	return tx.writeJournal("applying", nil)
}

func (tx *transaction) moveStructure(ctx context.Context) error {
	p := tx.plan
	switch p.Kind {
	case KindSubmodule, KindCampaignDir:
		if _, err := git.RunGitCmd(ctx, p.CampaignRoot, "mv", "--", p.OldPath, p.NewPath); err != nil {
			return camperrors.Wrap(err, "move project")
		}
		tx.structureMoved = true
		if p.Kind == KindSubmodule {
			newSection := p.NewPath
			if p.SubmoduleSection != newSection {
				if _, err := git.RunGitCmd(ctx, p.CampaignRoot, "config", "-f", ".gitmodules", "--rename-section", "submodule."+p.SubmoduleSection, "submodule."+newSection); err != nil {
					return camperrors.Wrap(err, "rename .gitmodules section")
				}
			}
			// Root-local config is optional for an uninitialized submodule.
			for _, oldSection := range uniqueStrings([]string{p.SubmoduleSection, p.OldPath}) {
				if configSectionExists(ctx, p.CampaignRoot, "submodule."+oldSection) {
					if _, err := git.RunGitCmd(ctx, p.CampaignRoot, "config", "--rename-section", "submodule."+oldSection, "submodule."+newSection); err != nil {
						return camperrors.Wrap(err, "rename local submodule config")
					}
					break
				}
			}
			if p.NewURL != "" {
				if _, err := git.RunGitCmd(ctx, p.CampaignRoot, "config", "-f", ".gitmodules", "submodule."+newSection+".url", p.NewURL); err != nil {
					return camperrors.Wrap(err, "update declared submodule URL")
				}
			}
		}
	case KindLinked:
		if err := os.Rename(tx.oldAbs(), tx.newAbs()); err != nil {
			return camperrors.Wrap(err, "rename linked project")
		}
		tx.structureMoved = true
	default:
		return camperrors.NewValidation("kind", "unsupported project kind", nil)
	}
	return nil
}

func (tx *transaction) migrateSubmoduleGitDir(ctx context.Context) error {
	p := tx.plan
	oldCommon, err := git.ResolveGitDir(tx.newAbs())
	if err != nil {
		return camperrors.Wrap(err, "resolve submodule Git directory")
	}
	rootGitDir, err := git.RunGitCmd(ctx, p.CampaignRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	expectedOld := filepath.Join(strings.TrimSpace(rootGitDir), "modules", filepath.FromSlash(p.OldPath))
	if filepath.Clean(oldCommon) != filepath.Clean(expectedOld) {
		return camperrors.NewValidation("submodule gitdir", "non-standard submodule Git directory cannot be moved safely", nil)
	}
	tx.commonOld = oldCommon
	tx.commonNew = filepath.Join(strings.TrimSpace(rootGitDir), "modules", filepath.FromSlash(p.NewPath))
	if _, err := os.Stat(tx.commonNew); err == nil {
		return camperrors.Wrapf(camperrors.ErrAlreadyExists, "submodule Git directory destination exists: %s", tx.commonNew)
	}
	if err := os.MkdirAll(filepath.Dir(tx.commonNew), 0o755); err != nil {
		return err
	}
	if err := os.Rename(tx.commonOld, tx.commonNew); err != nil {
		return camperrors.Wrap(err, "move submodule Git directory")
	}
	tx.commonMoved = true

	if err := writeGitdirPointer(filepath.Join(tx.newAbs(), ".git"), tx.commonNew); err != nil {
		return err
	}
	for _, change := range p.Worktrees {
		path := change.After
		if !change.Moved {
			path = change.Before
		}
		pointer := filepath.Join(path, ".git")
		data, err := os.ReadFile(pointer)
		if err != nil {
			return err
		}
		updated := strings.Replace(string(data), tx.commonOld, tx.commonNew, 1)
		if updated == string(data) {
			return camperrors.Newf("worktree pointer does not reference old submodule Git directory: %s", pointer)
		}
		if err := fsutil.WriteFileAtomically(pointer, []byte(updated), 0o644); err != nil {
			return err
		}
	}

	worktreeRel, err := filepath.Rel(tx.commonNew, tx.newAbs())
	if err != nil {
		return err
	}
	if err := runGitDir(ctx, tx.commonNew, "config", "core.worktree", worktreeRel); err != nil {
		return err
	}
	if _, err := git.RunGitCmd(ctx, p.CampaignRoot, "submodule", "sync", "--", p.NewPath); err != nil {
		return camperrors.Wrap(err, "sync renamed submodule")
	}
	return nil
}

func (tx *transaction) updateRemote(ctx context.Context, newURL string) error {
	if tx.plan.Kind == KindSubmodule {
		if _, err := git.RunGitCmd(ctx, tx.plan.CampaignRoot, "config", "-f", ".gitmodules", "submodule."+tx.plan.NewPath+".url", newURL); err != nil {
			return err
		}
		if _, err := git.RunGitCmd(ctx, tx.plan.CampaignRoot, "submodule", "sync", "--", tx.plan.NewPath); err != nil {
			return err
		}
	}
	if tx.plan.Initialized {
		if err := git.SetRemoteURL(ctx, tx.repositoryPathAfter(), "origin", newURL); err != nil {
			return camperrors.Wrap(err, "set project origin URL")
		}
		tx.remoteUpdated = true
	}
	return nil
}

func verify(ctx context.Context, plan *PlanResult) error {
	if _, err := os.Lstat(filepath.Join(plan.CampaignRoot, filepath.FromSlash(plan.OldPath))); !os.IsNotExist(err) {
		return camperrors.NewValidation("verification", "old project path still exists", err)
	}
	if _, err := os.Lstat(filepath.Join(plan.CampaignRoot, filepath.FromSlash(plan.NewPath))); err != nil {
		return camperrors.NewValidation("verification", "new project path is missing", err)
	}
	if plan.Kind == KindSubmodule {
		declared, err := declaredSubmodules(ctx, plan.CampaignRoot)
		if err != nil {
			return err
		}
		if _, oldExists := declared[plan.OldPath]; oldExists {
			return camperrors.NewValidation("verification", "old .gitmodules path remains", nil)
		}
		entry, exists := declared[plan.NewPath]
		if !exists || entry.Section != plan.NewPath {
			return camperrors.NewValidation("verification", "renamed .gitmodules identity is incomplete", nil)
		}
		if plan.Initialized {
			if _, err := git.RunGitCmd(ctx, filepath.Join(plan.CampaignRoot, filepath.FromSlash(plan.NewPath)), "status", "--porcelain"); err != nil {
				return camperrors.Wrap(err, "verify renamed submodule checkout")
			}
		}
	}
	for _, change := range plan.Worktrees {
		path := change.Before
		if change.Moved {
			path = change.After
		}
		if _, err := git.RunGitCmd(ctx, path, "status", "--porcelain"); err != nil {
			return camperrors.Wrap(err, "verify linked worktree")
		}
	}
	return nil
}

func (tx *transaction) rollback(ctx context.Context) error {
	var errs []error
	p := tx.plan
	if tx.remoteUpdated && tx.oldRemote != "" {
		if err := git.SetRemoteURL(ctx, tx.repositoryPathAfter(), "origin", tx.oldRemote); err != nil {
			errs = append(errs, err)
		}
	}
	if tx.snapshotsMoved {
		oldDir := filepath.Join(p.CampaignRoot, ".campaign", "leverage", "snapshots", p.OldName)
		newDir := filepath.Join(p.CampaignRoot, ".campaign", "leverage", "snapshots", p.NewName)
		if _, err := os.Stat(newDir); err == nil {
			if err := os.Rename(newDir, oldDir); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if tx.commonMoved {
		_ = writeGitdirPointer(filepath.Join(tx.newAbs(), ".git"), tx.commonOld)
		for _, change := range p.Worktrees {
			path := change.After
			if !change.Moved {
				path = change.Before
			}
			pointer := filepath.Join(path, ".git")
			if data, err := os.ReadFile(pointer); err == nil {
				updated := strings.Replace(string(data), tx.commonNew, tx.commonOld, 1)
				_ = fsutil.WriteFileAtomically(pointer, []byte(updated), 0o644)
			}
		}
		if err := os.Rename(tx.commonNew, tx.commonOld); err != nil {
			errs = append(errs, err)
		}
	}
	if tx.structureMoved {
		switch p.Kind {
		case KindSubmodule, KindCampaignDir:
			// git mv refuses to move a submodule while its automatically edited
			// .gitmodules file is unstaged. The transaction restores the exact
			// pre-operation index bytes below, so this temporary stage cannot
			// leak into the caller's index.
			if p.Kind == KindSubmodule {
				_, _ = git.RunGitCmd(ctx, p.CampaignRoot, "add", "--", ".gitmodules")
			}
			if _, err := git.RunGitCmd(ctx, p.CampaignRoot, "mv", "--", p.NewPath, p.OldPath); err != nil {
				errs = append(errs, err)
			}
		case KindLinked:
			if err := os.Rename(tx.newAbs(), tx.oldAbs()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for i := len(tx.movedWorktrees) - 1; i >= 0; i-- {
		change := tx.movedWorktrees[i]
		repo := tx.repositoryPathBefore()
		if err := worktree.NewGitWorktree(repo).Move(ctx, change.After, change.Before); err != nil {
			errs = append(errs, err)
		}
	}
	for _, backup := range tx.backups {
		if !backup.exists {
			_ = os.Remove(backup.path)
			continue
		}
		if err := fsutil.WriteFileAtomically(backup.path, backup.data, backup.mode); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (tx *transaction) writeJournal(status string, err error) error {
	record := journalRecord{
		Schema: "project-rename-journal/v1alpha1", OperationID: tx.plan.OperationID,
		Status: status, OldPath: tx.plan.OldPath, NewPath: tx.plan.NewPath,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err != nil {
		record.Error = err.Error()
	}
	data, marshalErr := json.MarshalIndent(record, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	return fsutil.WriteFileAtomically(tx.journal, append(data, '\n'), 0o600)
}

func (tx *transaction) oldAbs() string {
	return filepath.Join(tx.plan.CampaignRoot, filepath.FromSlash(tx.plan.OldPath))
}
func (tx *transaction) newAbs() string {
	return filepath.Join(tx.plan.CampaignRoot, filepath.FromSlash(tx.plan.NewPath))
}
func (tx *transaction) repositoryPathBefore() string {
	if tx.plan.Kind == KindLinked {
		return tx.plan.LinkedTarget
	}
	return tx.oldAbs()
}
func (tx *transaction) repositoryPathAfter() string {
	if tx.plan.Kind == KindLinked {
		return tx.plan.LinkedTarget
	}
	return tx.newAbs()
}

func configSectionExists(ctx context.Context, root, section string) bool {
	_, err := git.RunGitCmd(ctx, root, "config", "--get-regexp", "^"+regexpQuote(section)+`\.`)
	return err == nil
}

func regexpQuote(value string) string {
	replacer := strings.NewReplacer(".", `\.`, "+", `\+`, "*", `\*`, "?", `\?`, "[", `\[`, "]", `\]`, "(", `\(`, ")", `\)`)
	return replacer.Replace(value)
}

func runGitDir(ctx context.Context, gitDir string, args ...string) error {
	all := append([]string{"--git-dir", gitDir}, args...)
	cmd := exec.CommandContext(ctx, "git", all...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "git %s: %s", strings.Join(all, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func writeGitdirPointer(path, gitDir string) error {
	return fsutil.WriteFileAtomically(path, []byte("gitdir: "+gitDir+"\n"), 0o644)
}

func residualReferences(ctx context.Context, plan *PlanResult) []Reference {
	cmd := exec.CommandContext(ctx, "git", "-C", plan.CampaignRoot, "grep", "-n", "--", plan.OldName)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var refs []Reference
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if len(refs) == 20 || line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 3)
		ref := Reference{Path: parts[0]}
		if len(parts) > 1 {
			ref.Line, _ = strconv.Atoi(parts[1])
		}
		if len(parts) > 2 {
			ref.Text = strings.TrimSpace(parts[2])
		}
		refs = append(refs, ref)
	}
	return refs
}

func containsSnapshotChange(changes []MetadataChange) bool {
	for _, change := range changes {
		if change.Store == "leverage-snapshots" {
			return true
		}
	}
	return false
}
