package workitem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Split lineage marker keys. Lineage lives in the markers, not in links.yaml:
// the links registry attaches workitems to *scopes* — projects, worktrees — and
// not to each other. Overloading it would make "what is this workitem linked
// to" answer two unrelated questions.
const (
	// SplitIntoKey lists the successors a parent declared, on the parent.
	SplitIntoKey = "split_into"
	// SplitAtKey records when the split happened, on the parent.
	SplitAtKey = "split_at"
	// SplitFromKey names the parent, on each successor.
	SplitFromKey = "split_from"
	// SplitSeedHashKey records the digest of the README a split seeded, so
	// undo can prove a successor is untouched rather than guess.
	SplitSeedHashKey = "split_seed_hash"
)

// SplitSuccessor is one declared successor of a split.
type SplitSuccessor struct {
	// StableID is the successor's durable identity, and is what the parent's
	// split_into records and the retirement gate checks for.
	StableID string
	Ref      string
	Type     string
	// RelativePath is where the successor lives.
	RelativePath string
	// Created reports whether this split made it (--into) as opposed to
	// declaring an existing workitem (--adopt).
	Created bool
	// Adopted reports that a non-workitem directory was adopted to become one.
	Adopted bool
	// SeedHash is the digest of the README this split seeded, empty for a
	// successor that already existed.
	SeedHash string
}

// SplitLineage is the stamp a split writes, in both directions.
type SplitLineage struct {
	ParentStableID string
	ParentPath     string
	SuccessorIDs   []string
	At             time.Time
}

// ParentFields returns the lineage fields stamped onto the parent's marker.
//
// split_into is list-valued, which is why the marker stamper had to learn
// sequence nodes rather than gaining a second writer beside it.
func (l SplitLineage) ParentFields() []FrontmatterField {
	return []FrontmatterField{
		{After: "type", Key: SplitIntoKey, Values: l.SuccessorIDs},
		{After: SplitIntoKey, Key: SplitAtKey, Value: l.At.UTC().Format(time.RFC3339)},
	}
}

// SuccessorFields returns the lineage fields stamped onto one successor.
//
// seedHash is empty for an adopted successor, which had no seeded README and
// must never be deleted by undo: it pre-existed the split.
func (l SplitLineage) SuccessorFields(seedHash string) []FrontmatterField {
	fields := []FrontmatterField{
		{After: "type", Key: SplitFromKey, Value: l.ParentStableID},
	}
	if seedHash != "" {
		fields = append(fields, FrontmatterField{
			After: SplitFromKey, Key: SplitSeedHashKey, Value: seedHash,
		})
	}
	return fields
}

// SeedHash is the digest recorded for a seeded README, and the same function
// undo re-computes to decide whether a successor is still pristine.
func SeedHash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SplitLineageKeys are every key a split writes, which is exactly the set undo
// removes.
func SplitLineageKeys() []string {
	return []string{SplitIntoKey, SplitAtKey, SplitFromKey, SplitSeedHashKey}
}

// RecordSplitLineage stamps the parent and every successor.
//
// Idempotent: recordLifecycleFields replaces an existing key in place, so
// re-running a split that already stamped writes the same bytes rather than
// appending a second key. That matters because apply may retry a row.
//
// The parent is stamped last. A successor carrying split_from with no matching
// parent is a readable, recoverable state; a parent claiming successors that
// carry no back-link is the one that makes the retirement gate lie.
func RecordSplitLineage(ctx context.Context, root string, lineage SplitLineage, successors []SplitSuccessor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, successor := range successors {
		if err := recordLifecycleFields(ctx, root, successor.RelativePath, lineage.SuccessorFields(successor.SeedHash)); err != nil {
			return camperrors.Wrapf(err, "stamping split_from on %s", successor.RelativePath)
		}
	}
	if err := recordLifecycleFields(ctx, root, lineage.ParentPath, lineage.ParentFields()); err != nil {
		return camperrors.Wrapf(err, "stamping split_into on %s", lineage.ParentPath)
	}
	return nil
}

// SplitSpec is one parsed `--into` or `--adopt` argument.
type SplitSpec struct {
	// Value is the successor name (--into) or path (--adopt).
	Value string
	// Type overrides the parent's type for this successor, empty to inherit.
	Type string
}

// ParseSplitSpec parses `<value>[:<type>]`.
//
// A path may legitimately contain no colon, and a Windows-style drive letter is
// not a concern here because campaign paths are always campaign-relative. The
// split is on the LAST colon so a value may itself contain one.
func ParseSplitSpec(raw string) (SplitSpec, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return SplitSpec{}, camperrors.NewValidation("successor",
			"is empty", camperrors.ErrInvalidInput)
	}
	i := strings.LastIndex(trimmed, ":")
	if i < 0 {
		return SplitSpec{Value: trimmed}, nil
	}
	value, typeName := strings.TrimSpace(trimmed[:i]), strings.TrimSpace(trimmed[i+1:])
	if value == "" || typeName == "" {
		return SplitSpec{}, camperrors.NewValidation("successor",
			"expected <value> or <value>:<type>, got "+quoteSplit(raw), camperrors.ErrInvalidInput)
	}
	return SplitSpec{Value: value, Type: typeName}, nil
}

// ParseSplitSpecs parses every spec, reporting all malformed ones at once.
func ParseSplitSpecs(raws []string) ([]SplitSpec, error) {
	out := make([]SplitSpec, 0, len(raws))
	var problems []string
	for _, raw := range raws {
		spec, err := ParseSplitSpec(raw)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		out = append(out, spec)
	}
	if len(problems) > 0 {
		return nil, camperrors.NewValidation("successors",
			strings.Join(problems, "; "), camperrors.ErrInvalidInput)
	}
	return out, nil
}

// ValidateSplitSuccessors reports the rules a successor set must satisfy
// before anything is written.
//
// Checked up front rather than while creating, so a duplicate in the fifth
// argument does not leave four successors on disk and a half-stamped parent.
func ValidateSplitSuccessors(parentStableID string, specs []SplitSpec) error {
	if len(specs) == 0 {
		return camperrors.NewValidation("successors",
			"a split needs at least one successor: pass --into or --adopt",
			camperrors.ErrInvalidInput)
	}

	seen := make(map[string]bool, len(specs))
	var duplicates []string
	for _, spec := range specs {
		if seen[spec.Value] {
			duplicates = append(duplicates, spec.Value)
			continue
		}
		seen[spec.Value] = true
		if spec.Value == parentStableID {
			return camperrors.NewValidation("successors",
				"a workitem cannot be its own successor: "+quoteSplit(spec.Value),
				camperrors.ErrInvalidInput)
		}
	}
	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		return camperrors.NewValidation("successors",
			"named more than once: "+strings.Join(duplicates, ", "),
			camperrors.ErrInvalidInput)
	}
	return nil
}

// SplitReadme is the seeded README body for a created successor.
//
// The header is the trail: it says where this came from and when, in a form a
// human reading the successor cold can follow back. No content is moved
// automatically — deciding which part of a parent's scope belongs here is
// judgment, and a tool that guessed would produce successors nobody trusts.
func SplitReadme(successorTitle, parentTitle, parentRef string, at time.Time) []byte {
	var b strings.Builder
	b.WriteString("# " + successorTitle + "\n\n")

	b.WriteString("Split from ")
	if parentTitle != "" {
		b.WriteString("`" + parentTitle + "`")
	} else {
		b.WriteString("its parent")
	}
	if parentRef != "" {
		b.WriteString(" (`" + parentRef + "`)")
	}
	b.WriteString(" on " + at.UTC().Format("2006-01-02") + ".\n\n")

	b.WriteString("## Scope carried from parent\n\n")
	b.WriteString("_Move the part of the parent's scope that belongs here._\n")
	return []byte(b.String())
}

// SplitIntoOf reads the successors a workitem's marker declares.
func SplitIntoOf(meta *Metadata) []string {
	if meta == nil {
		return nil
	}
	return meta.SplitInto
}

// MissingSuccessors returns the declared successors that are not discoverable,
// in stable order.
//
// Existence, not content: whether a successor adequately captures the parent's
// scope is the split author's judgment, reviewed like any other work. Camp
// verifies the trail, not the prose.
func MissingSuccessors(declared []string, discovered map[string]bool) []string {
	var missing []string
	for _, id := range declared {
		if !discovered[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing
}

// ErrSplitGate is the sentinel a blocked terminal promotion returns.
var ErrSplitGate = camperrors.New("parent has unfinished successors")

// SplitGateError reports a terminal promotion refused because declared
// successors do not exist yet.
//
// This is the successors-before-archive invariant the field trial enforced as
// prose, made mechanical. It names the missing successors, because "blocked"
// without the list makes the operator go looking for what camp already knows.
func SplitGateError(parentStableID string, missing []string) error {
	return camperrors.WrapJoinf(ErrSplitGate, camperrors.ErrInvalidInput,
		"%s declared %s that %s not exist yet: %s\n"+
			"Create %s, or re-run with --force to retire the parent anyway",
		parentStableID,
		pluralSplit(len(missing), "a successor", "successors"),
		pluralVerb(len(missing)),
		strings.Join(missing, ", "),
		pluralSplit(len(missing), "it", "them"))
}

// pluralSplit picks a noun phrase for a count.
func pluralSplit(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// pluralVerb agrees with the count.
func pluralVerb(n int) string {
	if n == 1 {
		return "does"
	}
	return "do"
}

// quoteSplit wraps a value in double quotes for diagnostics.
func quoteSplit(s string) string { return "\"" + s + "\"" }

// StableIDOf resolves a workitem's durable identity the way the rest of camp
// does: the marker's id, else the source id, else the discovery key.
func StableIDOf(item any) string {
	switch w := item.(type) {
	case WorkItem:
		return stableIDOfItem(w)
	case *WorkItem:
		if w == nil {
			return ""
		}
		return stableIDOfItem(*w)
	}
	return ""
}

// stableIDOfItem is the resolution order itself.
func stableIDOfItem(item WorkItem) string {
	switch {
	case item.StableID != "":
		return item.StableID
	case item.SourceID != "":
		return item.SourceID
	default:
		return item.Key
	}
}

// UndoDisposition is what an undo did with one successor.
type UndoDisposition string

const (
	// UndoDeleted means the successor was pristine and was removed.
	UndoDeleted UndoDisposition = "deleted"
	// UndoKept means the successor was touched or pre-existing, so undo
	// removed its lineage stamp and left the directory alone.
	UndoKept UndoDisposition = "kept"
	// UndoMissing means the successor was already gone.
	UndoMissing UndoDisposition = "missing"
)

// UndoOutcome is one successor's result.
type UndoOutcome struct {
	StableID     string          `json:"stable_id"`
	RelativePath string          `json:"relative_path"`
	Disposition  UndoDisposition `json:"disposition"`
	// Reason explains a kept successor, so "kept" is never just a verdict.
	Reason string `json:"reason,omitempty"`
}

// PristineCheck is what undo needs to decide whether a successor is untouched.
type PristineCheck struct {
	// SeedHash is the digest recorded at split time, empty for an adopted
	// successor.
	SeedHash string
	// ReadmeHash is the digest of the README as it stands now.
	ReadmeHash string
	// ExtraEntries counts directory entries beyond the marker and README.
	ExtraEntries int
}

// Pristine reports whether a successor may be deleted by undo, and why not.
//
// Deletion is the destructive half of the inverse, so it is allowed only on
// proof: this successor was created by the split, its README still hashes to
// what the split seeded, and nobody has added anything beside it. Anything
// else is kept and unstamped, because undoing a mistake must not become a
// second, larger mistake.
func (c PristineCheck) Pristine() (bool, string) {
	if c.SeedHash == "" {
		return false, "it existed before the split, so undo only unstamps it"
	}
	if c.ReadmeHash != c.SeedHash {
		return false, "its README has been edited; dungeon it normally if unwanted"
	}
	if c.ExtraEntries > 0 {
		return false, "it has content beyond the seed; dungeon it normally if unwanted"
	}
	return true, ""
}
