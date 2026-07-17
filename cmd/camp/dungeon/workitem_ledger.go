package dungeon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
)

// recordWorkitemMove is the single shared code path every dungeon move
// command in this package routes through after the filesystem mutation
// succeeds, so new move entry points inherit campaign-wide workitem ledger
// coverage without re-implementing event construction. It checks the
// destination for .workitem metadata (the directory, including its marker,
// has already been relocated by the time this runs) and appends a "move"
// event when the moved item is a tracked workitem. Items without a marker
// (plain triage files or directories never adopted with camp workitem
// create/adopt) are silently skipped; a present-but-unreadable marker warns
// instead of failing the caller's already-applied move.
func recordWorkitemMove(ctx context.Context, campaignRoot, sourceAbs, destAbs string) {
	meta, err := wkitem.LoadMetadata(ctx, destAbs)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to read .workitem metadata for ledger event at %s: %v\n", destAbs, err)
		return
	}
	if meta == nil {
		return
	}

	wkaudit.AppendBestEffort(ctx, os.Stderr, campaignRoot, wkaudit.Event{
		Event: wkaudit.EventMove,
		ID:    meta.ID,
		Ref:   meta.Ref,
		Type:  meta.Type,
		Title: meta.Title,
		From:  filepath.ToSlash(RelFromRoot(campaignRoot, sourceAbs)),
		To:    filepath.ToSlash(RelFromRoot(campaignRoot, destAbs)),
	})
}

// workitemLedgerPathIfExists returns the absolute path to the campaign-wide
// workitem ledger and true when it already exists on disk. Callers stage the
// ledger into the same commit as a move only when it exists: `git add` hard
// fails the entire staging call if any pathspec in the list has never been
// created, even when every other path is valid, so a move that touched no
// workitem (and therefore never created or appended to the ledger) must not
// add its path to the commit's file list.
func workitemLedgerPathIfExists(campaignRoot string) (string, bool) {
	path := filepath.Join(campaignRoot, ".campaign", "workitems", wkaudit.AuditFile)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}
