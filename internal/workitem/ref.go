package workitem

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// RefPrefix is the literal prefix every workitem `ref` carries.
const RefPrefix = "WI-"

// Derive returns the canonical ref for a workitem id. The shape is stable:
// "WI-" plus the first 6 lowercase hex chars of sha256(id). Same id always
// yields the same ref; this is the reason the field is safe to recompute
// during migrations and doctor backfills.
func Derive(id string) string {
	sum := sha256.Sum256([]byte(id))
	return RefPrefix + hex.EncodeToString(sum[:])[:6]
}

// DeriveUnique returns a ref guaranteed not to collide with the keys of
// existing. On collision, the function re-rolls by hashing `id#1`, `id#2`,
// ... until a free slot is found. Returns the deterministic Derive(id)
// result whenever the original is already unique.
//
// The existing map is read-only; DeriveUnique does not insert into it.
// Callers should add the returned ref to their existing set before deriving
// another to keep the no-collision invariant.
func DeriveUnique(id string, existing map[string]bool) string {
	candidate := Derive(id)
	if !existing[candidate] {
		return candidate
	}
	for n := 1; ; n++ {
		candidate = Derive(fmt.Sprintf("%s#%d", id, n))
		if !existing[candidate] {
			return candidate
		}
	}
}

// RefsFromWorkitems returns the set of refs already in use across the
// provided WorkItem slice. Use this to seed DeriveUnique.
func RefsFromWorkitems(items []WorkItem) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		ref, ok := item.SourceMetadata["ref"].(string)
		if !ok || ref == "" {
			continue
		}
		out[ref] = true
	}
	return out
}
