package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dirlink"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/google/uuid"
)

type LinkRepairState struct {
	CampaignID string `json:"campaignId"`
	Name       string `json:"name"`
	OldTarget  string `json:"oldTarget"`
	NewTarget  string `json:"newTarget"`
	OriginKey  string `json:"originKey,omitempty"`
}

// ApplyLinkRepair replaces only a still-broken link after rechecking the
// preview, candidate, and original target under a local repair lock.
func ApplyLinkRepair(ctx context.Context, campaignRoot string, reviewed *RepairPreview) (*LinkRepairState, error) {
	if reviewed == nil || reviewed.Selected == nil {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "select a checkout with --path before applying repair")
	}
	return withRepairLock(ctx, campaignRoot, reviewed.Name, func(statePath string) (*LinkRepairState, error) {
		if _, err := os.Stat(statePath); err == nil {
			return nil, camperrors.Wrap(camperrors.ErrConflict, "repair undo state already exists; undo it before applying another repair")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		fresh, err := PreviewLinkRepair(ctx, campaignRoot, reviewed.Name, reviewed.Selected.Path)
		if err != nil {
			return nil, err
		}
		if fresh.OldTarget != reviewed.OldTarget || fresh.Path != reviewed.Path || fresh.Selected == nil ||
			fresh.Selected.Path != reviewed.Selected.Path || fresh.Selected.OriginKey != reviewed.Selected.OriginKey ||
			fresh.Selected.OwnerID != reviewed.Selected.OwnerID {
			return nil, camperrors.Wrap(camperrors.ErrConflict, "project link or selected checkout changed after preview")
		}
		if runtime.GOOS == "windows" && !filepath.IsAbs(fresh.OldTarget) {
			return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "Windows repair requires an absolute saved link target so undo can preserve it")
		}
		cfg, err := config.ReadCampaignConfig(ctx, campaignRoot)
		if err != nil {
			return nil, err
		}
		state := &LinkRepairState{CampaignID: cfg.ID, Name: fresh.Name, OldTarget: fresh.OldTarget, NewTarget: fresh.Selected.Path, OriginKey: fresh.Selected.OriginKey}
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := fsutil.WriteFileAtomically(statePath, data, 0o600); err != nil {
			if removeErr := os.Remove(statePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return nil, errors.Join(camperrors.Wrap(err, "save link repair undo state"), removeErr)
			}
			return nil, camperrors.Wrap(err, "save link repair undo state")
		}
		linkPath := filepath.Join(campaignRoot, fresh.Path)
		if err := swapRepairLink(linkPath, fresh.OldTarget, fresh.Selected.Path, fresh.Selected.Path, true); err != nil {
			// Keep undo evidence if rollback itself failed or the new link
			// remains in place. Only discard it once the old link is verified.
			if current, readErr := dirlink.ReadTarget(linkPath); readErr == nil && current == fresh.OldTarget {
				if removeErr := os.Remove(statePath); removeErr != nil {
					return nil, errors.Join(err, removeErr)
				}
			}
			return nil, err
		}
		return state, nil
	})
}

// UndoLinkRepair restores the exact saved target after verifying that the
// current link is still the one written by ApplyLinkRepair.
func UndoLinkRepair(ctx context.Context, campaignRoot, name string) (*LinkRepairState, error) {
	return withRepairLock(ctx, campaignRoot, name, func(statePath string) (*LinkRepairState, error) {
		data, err := os.ReadFile(statePath)
		if err != nil {
			return nil, camperrors.Wrap(err, "read link repair undo state")
		}
		var state LinkRepairState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, camperrors.Wrap(err, "parse link repair undo state")
		}
		cfg, err := config.ReadCampaignConfig(ctx, campaignRoot)
		if err != nil {
			return nil, err
		}
		if state.CampaignID != cfg.ID || state.Name != name {
			return nil, camperrors.Wrap(camperrors.ErrConflict, "repair undo state belongs to another camp or project")
		}
		linkPath := filepath.Join(campaignRoot, "projects", name)
		if current, err := dirlink.ReadTarget(linkPath); err == nil && current == state.OldTarget {
			// A process may stop after saving undo state but before changing
			// the link. Clear the stale state without touching the old entry.
			if err := os.Remove(statePath); err != nil {
				return &state, err
			}
			return &state, nil
		}
		if err := swapRepairLink(linkPath, state.NewTarget, storedTargetPath(linkPath, state.OldTarget), state.OldTarget, false); err != nil {
			return nil, err
		}
		if err := os.Remove(statePath); err != nil {
			return &state, camperrors.Wrap(err, "remove used link repair undo state")
		}
		return &state, nil
	})
}

func withRepairLock(ctx context.Context, root, name string, run func(string) (*LinkRepairState, error)) (*LinkRepairState, error) {
	if err := ValidateProjectName(name); err != nil {
		return nil, err
	}
	if _, err := config.ReadCampaignConfig(ctx, root); err != nil {
		return nil, err
	}
	registryPath, err := config.RegistryPath()
	if err != nil {
		return nil, err
	}
	stateDir := filepath.Join(filepath.Dir(registryPath), "link-repairs")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, camperrors.Wrap(err, "create repair state directory")
	}
	lockPath := filepath.Join(stateDir, "repair.lock")
	release, err := fsutil.AcquireFileLock(ctx, lockPath)
	if err != nil {
		return nil, err
	}
	defer release()
	cfg, err := config.ReadCampaignConfig(ctx, root)
	if err != nil {
		return nil, err
	}
	return run(filepath.Join(stateDir, cfg.ID+"-"+name+".json"))
}

func swapRepairLink(linkPath, expected, targetPath, storeTarget string, requireTarget bool) error {
	st, err := dirlink.Lstat(linkPath)
	if err != nil {
		return err
	}
	if !st.IsDirectoryLink() {
		return camperrors.Wrap(camperrors.ErrConflict, "project entry is no longer a link")
	}
	old, err := dirlink.ReadTarget(linkPath)
	if err != nil {
		return err
	}
	if old != expected {
		return camperrors.Wrapf(camperrors.ErrConflict, "project link changed from reviewed target %q to %q", expected, old)
	}
	newPath := linkPath + ".repair-new-" + uuid.NewString()
	backupPath := linkPath + ".repair-old-" + uuid.NewString()
	if err := dirlink.Create(dirlink.CreateOptions{LinkPath: newPath, TargetPath: targetPath, StoreTarget: storeTarget}); err != nil {
		return err
	}
	defer func() { _ = dirlink.Remove(newPath) }()
	if err := os.Rename(linkPath, backupPath); err != nil {
		return camperrors.Wrap(err, "save old project link")
	}
	rollback := func(cause error) error {
		if _, err := os.Lstat(linkPath); err == nil {
			current, readErr := dirlink.ReadTarget(linkPath)
			if readErr != nil || current != storeTarget {
				return errors.Join(cause, camperrors.Wrapf(camperrors.ErrConflict, "another entry appeared at %s; old link retained at %s", linkPath, backupPath))
			}
			if removeErr := dirlink.Remove(linkPath); removeErr != nil {
				return errors.Join(cause, removeErr)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.Join(cause, err)
		}
		if err := os.Rename(backupPath, linkPath); err != nil {
			return errors.Join(cause, camperrors.Wrap(err, "restore old project link"))
		}
		return cause
	}
	if saved, err := dirlink.ReadTarget(backupPath); err != nil || saved != expected {
		return rollback(camperrors.Wrapf(camperrors.ErrConflict, "project link changed during replacement: %v, %q", err, saved))
	}
	// Create at the final path so an entry inserted by another process is
	// never overwritten by os.Rename's replacement semantics on Unix.
	if err := dirlink.Create(dirlink.CreateOptions{LinkPath: linkPath, TargetPath: targetPath, StoreTarget: storeTarget}); err != nil {
		return rollback(camperrors.Wrap(err, "activate repaired project link"))
	}
	if requireTarget {
		if info, err := os.Stat(linkPath); err != nil || !info.IsDir() {
			return rollback(camperrors.Wrapf(camperrors.ErrConflict, "repaired target disappeared: %v", err))
		}
	}
	if err := dirlink.Remove(backupPath); err != nil {
		return rollback(camperrors.Wrap(err, "remove saved old link after repair"))
	}
	return nil
}

func storedTargetPath(linkPath, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(linkPath), target)
}

func RepairStateDescription(state *LinkRepairState) string {
	return fmt.Sprintf("projects/%s: %s → %s (undo: camp project repair %s --undo)", state.Name, state.OldTarget, state.NewTarget, state.Name)
}
