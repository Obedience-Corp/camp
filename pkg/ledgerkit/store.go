package ledgerkit

import (
	"io"
	"os"
	"path/filepath"
	"sort"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// shardRef identifies one shard file by its path relative to the campaign root
// and its YYYY-MM month bucket. Rel is the stable key used in cursors and
// diagnostics; Month drives time-window pushdown.
type shardRef struct {
	Rel   string
	Month string
}

// shardStore abstracts the filesystem access the reader needs: enumerating
// shards, sizing them, and reading a shard's bytes from an offset. It is
// injected so Query/ReadSince/Read are unit-testable with an in-memory store,
// keeping filesystem-mutating behavior out of host unit tests (the repo's
// no-host-fs-tests rule). The default osShardStore is exercised on real disk by
// CLI-level integration once commands emit and consumers read (seq 06).
type shardStore interface {
	// list returns every shard, sorted by Rel.
	list() ([]shardRef, error)
	// size returns the current byte length of a shard.
	size(rel string) (int64, error)
	// readFrom returns the shard's bytes from offset to end. A missing shard
	// yields (nil, nil): an absent ledger reads as empty.
	readFrom(rel string, offset int64) ([]byte, error)
}

// osShardStore is the real filesystem-backed shard store rooted at a campaign.
type osShardStore struct {
	campaignRoot string
}

func (s osShardStore) abs(rel string) string { return filepath.Join(s.campaignRoot, rel) }

func (s osShardStore) list() ([]shardRef, error) {
	pattern := filepath.Join(s.campaignRoot, EventsDir, "*", "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, camperrors.Wrapf(err, "ledgerkit: glob shards under %s", s.campaignRoot)
	}
	sort.Strings(matches)
	refs := make([]shardRef, 0, len(matches))
	for _, m := range matches {
		rel, relErr := filepath.Rel(s.campaignRoot, m)
		if relErr != nil {
			rel = m
		}
		refs = append(refs, shardRef{Rel: rel, Month: filepath.Base(filepath.Dir(m))})
	}
	return refs, nil
}

func (s osShardStore) size(rel string) (int64, error) {
	info, err := os.Stat(s.abs(rel))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, camperrors.Wrapf(err, "ledgerkit: stat shard %s", rel)
	}
	return info.Size(), nil
}

func (s osShardStore) readFrom(rel string, offset int64) ([]byte, error) {
	f, err := os.Open(s.abs(rel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, camperrors.Wrapf(err, "ledgerkit: open shard %s", rel)
	}
	defer func() { _ = f.Close() }()
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, camperrors.Wrapf(err, "ledgerkit: seek shard %s", rel)
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, camperrors.Wrapf(err, "ledgerkit: read shard %s", rel)
	}
	return data, nil
}
