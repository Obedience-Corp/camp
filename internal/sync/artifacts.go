package sync

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/rsyncprobe"
)

// pullArtifacts transfers declared artifact roots from the configured peer.
// Default syncs move policy=always roots; an explicit --artifacts-only run
// moves every declared root and treats per-root failures as sync failures
// (the artifacts were the whole ask).
func (s *Syncer) pullArtifacts(ctx context.Context, result *SyncResult) {
	if s.peer == nil {
		result.Success = false
		result.Errors = append(result.Errors, &SyncError{Op: "artifacts",
			Cause: camperrors.New("artifact sync needs a peer (--from <machine>)")})
		return
	}
	prober := rsyncprobe.NewProber(s.options.NoProbeCache)
	cfg, err := artifacts.Load(s.repoRoot)
	if err != nil {
		// A default `--from` sync degrades artifact failures to warnings (the
		// accelerated path must never be worse than a plain sync), but under
		// --artifacts-only the artifacts were the entire ask, so an unreadable
		// config is a hard failure rather than a silent exit 0.
		result.Warnings = append(result.Warnings, fmt.Sprintf("artifacts config: %v", err))
		if s.options.ArtifactsOnly {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts", Cause: err})
		}
		return
	}
	// A committed declaration is not trusted just because it parsed: an
	// unsupported version, unknown policy, or duplicate root must fail closed
	// here rather than be silently skipped on a normal sync yet pulled under
	// --artifacts-only. Same degrade contract as an unreadable config.
	if verr := cfg.Validate(); verr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("artifacts config: %v", verr))
		if s.options.ArtifactsOnly {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts", Cause: verr})
		}
		return
	}
	for _, root := range cfg.Roots {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, ctx.Err())
			result.Success = false
			return
		}
		if !s.options.ArtifactsOnly && root.EffectivePolicy() != artifacts.PolicyAlways {
			continue
		}
		pr := artifacts.PullWithProber(ctx, s.repoRoot, s.peer, root, prober)
		result.Artifacts = append(result.Artifacts, pr)
		// The top-of-loop guard cannot catch a cancellation that lands during
		// this root's transfer (the last root has no next iteration), so
		// re-check here: a cancelled sync must fail regardless of mode rather
		// than reporting the interrupted transfer as a warning and exiting 0.
		if ctx.Err() != nil {
			result.Success = false
			result.Errors = append(result.Errors, ctx.Err())
			return
		}
		if pr.Warning != "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("artifacts %s: %s", pr.Root, pr.Warning))
			if s.options.ArtifactsOnly {
				result.Success = false
			}
		}
	}
}

// verifyArtifacts compares each declared root against last-transfer
// snapshots: no transfer, no git phases. Discrepancies fail the run so
// scripting can rely on the exit code.
func (s *Syncer) verifyArtifacts(ctx context.Context, result *SyncResult) {
	// VerifyPeer comes straight from --from and is joined into the snapshot
	// path (.campaign/cache/peersync/<peer>/...); validate it before it can
	// traverse on read (e.g. --verify-artifacts --from ../../..).
	if s.options.VerifyPeer != "" {
		if err := artifacts.ValidatePeerID(s.options.VerifyPeer); err != nil {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Cause: err})
			return
		}
	}

	cfg, err := artifacts.Load(s.repoRoot)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Cause: err})
		return
	}
	if err := cfg.Validate(); err != nil {
		result.Success = false
		result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Cause: err})
		return
	}

	peers := []string{s.options.VerifyPeer}
	if s.options.VerifyPeer == "" {
		peers, err = artifacts.ListSnapshotPeers(s.repoRoot)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Cause: err})
			return
		}
		if len(peers) == 0 {
			result.Warnings = append(result.Warnings,
				"no peer snapshots recorded yet; nothing to verify against (run 'camp sync --from <machine>' first)")
			return
		}
	}

	matched := 0
	for _, root := range cfg.Roots {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, ctx.Err())
			result.Success = false
			return
		}
		rootRel, err := artifacts.EnsureRootWithin(s.repoRoot, root.Path)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Submodule: root.Path, Cause: err})
			continue
		}
		local, err := artifacts.BuildManifest(ctx, s.repoRoot, rootRel)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Submodule: rootRel, Cause: err})
			continue
		}
		for _, peerID := range peers {
			snapshot, err := artifacts.LoadSnapshot(s.repoRoot, peerID, rootRel)
			if err != nil {
				result.Success = false
				result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Submodule: rootRel, Cause: err})
				continue
			}
			if snapshot == nil {
				continue
			}
			matched++
			verify := artifacts.Verify(local, snapshot)
			result.ArtifactVerifies = append(result.ArtifactVerifies, ArtifactVerify{Peer: peerID, Result: verify})
			if !verify.Clean() {
				result.Success = false
			}
		}
	}

	// A --from-scoped verify that matched no snapshot for any root is almost
	// always a typo'd or never-synced peer id: report it rather than exiting
	// 0 "complete" having checked nothing.
	if s.options.VerifyPeer != "" && matched == 0 && len(cfg.Roots) > 0 {
		result.Success = false
		result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify",
			Cause: camperrors.Newf("no snapshots recorded for peer %q; check the machine id or run 'camp sync --from %s' first",
				s.options.VerifyPeer, s.options.VerifyPeer)})
	}
}
