package statuspath

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

// DateDir returns the YYYY-MM-DD directory name used for dated dungeon buckets.
func DateDir(t time.Time) string {
	return t.Format(dateLayout)
}

// DatedDir returns the dated status directory for a move timestamp.
func DatedDir(statusRoot string, t time.Time) string {
	return filepath.Join(statusRoot, DateDir(t))
}

// DatedItemPath returns the dated destination path for an item in a status directory.
func DatedItemPath(statusRoot, itemName string, t time.Time) string {
	return filepath.Join(DatedDir(statusRoot, t), itemName)
}

// ExistingItemPath resolves an item in a status directory, supporting both the
// legacy flat layout (status/item) and the dated layout (status/YYYY-MM-DD/item).
func ExistingItemPath(statusRoot, itemName string) (string, bool, error) {
	legacyPath := filepath.Join(statusRoot, itemName)
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath, true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}

	entries, err := os.ReadDir(statusRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}

	var datedDirs []string
	for _, entry := range entries {
		if entry.IsDir() && IsDateDir(entry.Name()) {
			datedDirs = append(datedDirs, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(datedDirs)))

	for _, dirName := range datedDirs {
		candidate := filepath.Join(statusRoot, dirName, itemName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", false, err
		}
	}

	return "", false, nil
}

// CountItems counts logical items in a status directory while treating dated
// bucket directories as containers rather than items themselves.
func CountItems(statusRoot string) (int, error) {
	entries, err := os.ReadDir(statusRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if shouldSkip(entry.Name()) {
			continue
		}

		if entry.IsDir() && IsDateDir(entry.Name()) {
			subEntries, err := os.ReadDir(filepath.Join(statusRoot, entry.Name()))
			if err != nil {
				return 0, err
			}
			for _, sub := range subEntries {
				if shouldSkip(sub.Name()) {
					continue
				}
				count++
			}
			continue
		}

		count++
	}

	return count, nil
}

// IsDateDir reports whether a directory name matches the YYYY-MM-DD date-bucket format.
func IsDateDir(name string) bool {
	if len(name) != len(dateLayout) {
		return false
	}
	_, err := time.Parse(dateLayout, name)
	return err == nil
}

func shouldSkip(name string) bool {
	return name == ".gitkeep" || strings.HasPrefix(name, ".")
}
