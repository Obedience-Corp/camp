package complete

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// applyRecentFirstOrder reorders candidate names so that entries backed by a
// .workitem marker come first, sorted by marker mtime descending. Entries
// without a marker keep their original relative order (stable). When no entry
// has a marker, the input is returned unchanged.
//
// absDir is the directory that names are relative to. names may have a
// trailing slash (preserved in the output).
func applyRecentFirstOrder(absDir string, names []string) []string {
	if len(names) == 0 {
		return names
	}
	type ranked struct {
		name       string
		idx        int
		isWorkitem bool
		modTime    time.Time
	}
	entries := make([]ranked, len(names))
	hasItems := false
	for i, n := range names {
		entries[i] = ranked{name: n, idx: i}
		base := strings.TrimRight(n, "/")
		marker := filepath.Join(absDir, base, ".workitem")
		info, err := os.Stat(marker)
		if err != nil {
			continue
		}
		entries[i].isWorkitem = true
		entries[i].modTime = info.ModTime()
		hasItems = true
	}
	if !hasItems {
		return names
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.isWorkitem != b.isWorkitem {
			return a.isWorkitem
		}
		if a.isWorkitem && !a.modTime.Equal(b.modTime) {
			return a.modTime.After(b.modTime)
		}
		return a.idx < b.idx
	})
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.name
	}
	return out
}
