package triage

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// Repair is one identity the preflight gave a workitem that lacked one.
type Repair struct {
	// RelPath is the directory that was adopted, campaign-relative.
	RelPath string `json:"relative_path"`
	Type    string `json:"type"`
	ID      string `json:"id"`
	Ref     string `json:"ref"`
}

// PreflightResult reports what the identity preflight did.
type PreflightResult struct {
	// Repaired lists the adoptions performed, in path order.
	Repaired []Repair `json:"repaired"`
	// Unrepaired lists rows that still lack a durable identity. Under the
	// repair policy this holds only what repair could not fix.
	Unrepaired []string `json:"unrepaired"`
}

// PreflightInput is what the identity preflight needs to run.
type PreflightInput struct {
	CampaignRoot string
	// Items are the in-scope workitems. The preflight rewrites the entries it
	// adopts in place, so the snapshot that follows sees the new identities.
	Items []workitem.WorkItem
	// AllItems is every discovered workitem, used to keep generated ids and
	// refs unique campaign-wide rather than only within the scope.
	AllItems []workitem.WorkItem
	Policy   IdentityPolicy
	// Now dates generated ids, injected so a repair is reproducible.
	Now time.Time
}

// Preflight gives a durable identity to in-scope workitems that lack one,
// before the manifest freezes.
//
// This exists because a triage verdict eventually moves something. A row
// identified only by its path cannot be moved safely: the path is the thing
// that changes. FT-008 found three such items in a 20-row trial, so the
// default is to close the gap rather than to report it and stop — camp handles
// it and reports, which is why every adoption comes back in the result and the
// caller prints it.
//
// Adoption goes through workitem.AdoptDirectory, the same call
// `camp workitem adopt` makes, so a repaired item is indistinguishable from a
// hand-adopted one.
func Preflight(ctx context.Context, in PreflightInput) (*PreflightResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	needing := make([]int, 0)
	for i := range in.Items {
		if workitem.NeedsAdoption(&in.Items[i]) {
			needing = append(needing, i)
		}
	}
	result := &PreflightResult{Repaired: []Repair{}, Unrepaired: []string{}}
	if len(needing) == 0 {
		return result, nil
	}

	// Deterministic order: adoption generates ids and refs against a set that
	// grows as it goes, so the order decides which item gets a re-rolled ref
	// on a collision.
	sort.Slice(needing, func(a, b int) bool {
		return in.Items[needing[a]].RelativePath < in.Items[needing[b]].RelativePath
	})

	if in.Policy == IdentityPolicyStrict {
		for _, i := range needing {
			result.Unrepaired = append(result.Unrepaired, in.Items[i].RelativePath)
		}
		return result, strictIdentityError(result.Unrepaired)
	}

	existingIDs := workitem.IDsFromWorkitems(in.AllItems)
	existingRefs := workitem.RefsFromWorkitems(in.AllItems)

	for _, i := range needing {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := in.Items[i]

		id := uniqueAdoptID(item, in.Now, existingIDs)
		ref, err := workitem.DeriveUnique(ctx, id, existingRefs)
		if err != nil {
			return nil, camperrors.Wrapf(err, "derive ref for %s", item.RelativePath)
		}

		meta, err := workitem.AdoptDirectory(ctx, in.CampaignRoot, workitem.AdoptRequest{
			RelPath: item.RelativePath,
			Type:    string(item.WorkflowType),
			Title:   item.Title,
			ID:      id,
			Ref:     ref,
		})
		if err != nil {
			return nil, camperrors.Wrapf(err, "adopt %s", item.RelativePath)
		}
		existingIDs[id] = true
		existingRefs[ref] = true

		// Reflect the new identity into the item the snapshot will read, so
		// the manifest records the repaired id rather than the path fallback.
		in.Items[i].StableID = meta.ID
		if in.Items[i].SourceMetadata == nil {
			in.Items[i].SourceMetadata = map[string]any{}
		}
		in.Items[i].SourceMetadata["ref"] = meta.Ref

		result.Repaired = append(result.Repaired, Repair{
			RelPath: item.RelativePath,
			Type:    string(item.WorkflowType),
			ID:      meta.ID,
			Ref:     meta.Ref,
		})
	}
	return result, nil
}

// uniqueAdoptID builds the id for an adopted directory, matching the shape
// `camp workitem create` and `adopt` generate (<type>-<slug>-<date>), and
// re-rolls with a numeric suffix on collision.
func uniqueAdoptID(item workitem.WorkItem, now time.Time, existing map[string]bool) string {
	slug := filepath.Base(item.RelativePath)
	base := string(item.WorkflowType) + "-" + slug + "-" + now.UTC().Format("2006-01-02")
	if !existing[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := base + "-" + strconv.Itoa(n)
		if !existing[candidate] {
			return candidate
		}
	}
}

// strictIdentityError refuses the run and names every path to adopt, so the
// operator can fix all of them in one pass instead of rediscovering them one
// run at a time.
func strictIdentityError(paths []string) error {
	var b strings.Builder
	b.WriteString("identity policy is strict and ")
	b.WriteString(strconv.Itoa(len(paths)))
	if len(paths) == 1 {
		b.WriteString(" workitem has no .workitem marker:")
	} else {
		b.WriteString(" workitems have no .workitem marker:")
	}
	for _, p := range paths {
		b.WriteString("\n  ")
		b.WriteString(p)
	}
	return camperrors.NewValidation("preflight.identity", b.String(), camperrors.ErrInvalidInput)
}
