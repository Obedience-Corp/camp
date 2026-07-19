package workitem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/ledger"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// runAdoptFile stamps kind: workitem frontmatter onto an existing markdown file
// without ever rewriting the body. It has three branches: prepend a full block
// when the file has no frontmatter, merge camp's keys into a foreign block, or
// update tags/projects when the file is already a workitem.
func runAdoptFile(ctx context.Context, cmd *cobra.Command, filePath, typeFlag, title, idOverride, questSelector string, tags, projects []string, force bool) error {
	if err := validateSlug(typeFlag); err != nil {
		return camperrors.NewValidation("type", "invalid type slug: "+err.Error(), nil)
	}
	normalizedTags, err := normalizeTags(tags)
	if err != nil {
		return err
	}
	normalizedProjects, err := normalizeProjects(projects)
	if err != nil {
		return err
	}
	if err := wkitem.ValidateProjectPaths(normalizedProjects); err != nil {
		return err
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	rel := filePath
	if filepath.IsAbs(filePath) {
		rel, err = filepath.Rel(campaignRoot, filePath)
		if err != nil {
			return camperrors.Wrap(err, "resolve file relative to campaign root")
		}
	}
	if err := validateParentPath(rel); err != nil {
		return err
	}
	abs := filepath.Join(campaignRoot, rel)

	info, err := os.Stat(abs)
	if err != nil {
		return camperrors.Wrap(err, "stat target file")
	}
	if info.IsDir() {
		return camperrors.NewValidation("file",
			"target is a directory; run `camp workitem adopt "+rel+"` without --file", nil)
	}

	content, err := os.ReadFile(abs)
	if err != nil {
		return camperrors.Wrapf(err, "read %s", rel)
	}

	// Already a workitem: update in place (identity is fixed; tags/projects merge).
	existing, lerr := wkitem.LoadFrontmatterMetadata(abs)
	if lerr != nil {
		return camperrors.Wrapf(lerr, "existing frontmatter in %s is invalid", rel)
	}
	if existing != nil {
		return updateAdoptedFile(ctx, cmd, campaignRoot, rel, abs, content, existing, idOverride, normalizedTags, normalizedProjects)
	}

	slug := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	id, err := generateID(ctx, typeFlag, slug, idOverride, campaignRoot)
	if err != nil {
		return err
	}
	ref, err := deriveUniqueRef(ctx, campaignRoot, cfg, id)
	if err != nil {
		return err
	}
	questID := resolveQuestIDForCreate(ctx, cmd, campaignRoot, questSelector)

	_, _, hasFrontmatter := wkitem.SplitFrontmatter(content)
	if !hasFrontmatter {
		meta := wkitem.Metadata{
			Version:  wkitem.WorkitemSchemaVersion,
			Kind:     "workitem",
			ID:       id,
			Type:     typeFlag,
			Title:    title,
			Ref:      ref,
			QuestID:  questID,
			Tags:     normalizedTags,
			Projects: normalizedProjects,
		}
		fmBlock, err := wkitem.MarshalMetadataFrontmatter(&meta)
		if err != nil {
			return err
		}
		if err := prependFrontmatterLocked(ctx, abs, wkitem.FenceFrontmatter(fmBlock)); err != nil {
			return err
		}
	} else {
		tagsToStamp := normalizedTags
		if wkitem.FrontmatterHasKey(content, "tags") {
			if !force {
				return camperrors.NewValidation("tags",
					"file "+rel+" already has a foreign `tags:` frontmatter key; re-run with --force to take ownership (conforming values are kept, non-conforming ones dropped)", nil)
			}
			merged, dropped := mergeForeignTags(content, normalizedTags)
			tagsToStamp = merged
			if len(dropped) > 0 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"note: dropped %d non-conforming foreign tag value(s) from %s: %s\n",
					len(dropped), rel, strings.Join(dropped, ", "))
			}
		}
		warnReflow(cmd, content)
		hasTitle := wkitem.FrontmatterHasKey(content, "title")
		if err := wkitem.StampFrontmatterFields(ctx, abs,
			campStampFields(id, typeFlag, title, ref, questID, tagsToStamp, normalizedProjects, hasTitle)); err != nil {
			return err
		}
	}

	emitFileAdopt(ctx, cmd, campaignRoot, rel, id, ref, typeFlag, title, questID)
	return nil
}

// updateAdoptedFile handles a file that already carries kind: workitem: refuse a
// conflicting --id, otherwise union new tags/projects into the existing lists and
// re-stamp only when something changed (keeping re-runs diff-clean).
func updateAdoptedFile(ctx context.Context, cmd *cobra.Command, campaignRoot, rel, abs string, content []byte, existing *wkitem.Metadata, idOverride string, newTags, newProjects []string) error {
	if idOverride != "" && idOverride != existing.ID {
		return camperrors.NewValidation("id",
			"file "+rel+" already declares id "+existing.ID+"; refusing to overwrite with --id "+idOverride, nil)
	}

	mergedTags := unionStrings(existing.Tags, newTags)
	mergedProjects := unionStrings(existing.Projects, newProjects)

	var fields []wkitem.FrontmatterField
	if !equalStrings(existing.Tags, mergedTags) {
		fields = append(fields, wkitem.FrontmatterField{After: "ref", Key: "tags", Values: mergedTags})
	}
	if !equalStrings(existing.Projects, mergedProjects) {
		fields = append(fields, wkitem.FrontmatterField{After: "tags", Key: "projects", Values: mergedProjects})
	}
	if len(fields) > 0 {
		warnReflow(cmd, content)
		if err := wkitem.StampFrontmatterFields(ctx, abs, fields); err != nil {
			return err
		}
	}
	emitFileAdopt(ctx, cmd, campaignRoot, rel, existing.ID, existing.Ref, existing.Type, existing.Title, existing.QuestID)
	return nil
}

// campStampFields lists camp's keys in stable order for a foreign-frontmatter
// merge. version appends after the foreign keys (After ""), and each subsequent
// key chains after the previous camp key so a skipped optional key never breaks
// the chain. An existing foreign title is left untouched.
func campStampFields(id, typeFlag, title, ref, questID string, tags, projects []string, hasTitle bool) []wkitem.FrontmatterField {
	var fields []wkitem.FrontmatterField
	prev := ""
	add := func(key, value string, values []string) {
		fields = append(fields, wkitem.FrontmatterField{After: prev, Key: key, Value: value, Values: values})
		prev = key
	}
	add("version", wkitem.WorkitemSchemaVersion, nil)
	add("kind", "workitem", nil)
	add("id", id, nil)
	add("type", typeFlag, nil)
	if !hasTitle && title != "" {
		add("title", title, nil)
	}
	add("ref", ref, nil)
	if questID != "" {
		add("quest_id", questID, nil)
	}
	if len(tags) > 0 {
		add("tags", "", tags)
	}
	if len(projects) > 0 {
		add("projects", "", projects)
	}
	return fields
}

func emitFileAdopt(ctx context.Context, cmd *cobra.Command, campaignRoot, rel, id, ref, typeFlag, title, questID string) {
	invalidateNavigationCache(cmd, campaignRoot)
	appendWorkitemAuditEvent(ctx, cmd, campaignRoot, wkaudit.Event{
		Event: wkaudit.EventAdopt,
		ID:    id,
		Ref:   ref,
		Type:  typeFlag,
		Title: title,
		To:    filepath.ToSlash(rel),
	})
	ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindCreated, ledgerkit.Scope{Workitem: ref, Quest: questID},
			ledger.WithWhy(title),
			ledger.WithPayload(map[string]any{"type": typeFlag, "title": title, "path": rel, "adopted": true, "file": true}))
	questLine := ""
	if questID != "" {
		questLine = fmt.Sprintf("  quest: %s\n", questID)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(),
		"adopted file %s\n  id: %s\n  ref: %s\n  type: %s\n%s",
		rel, id, ref, typeFlag, questLine)
}

func writeFileLocked(ctx context.Context, abs string, content []byte) error {
	release, err := fsutil.AcquireFileLock(ctx, abs+".lock")
	if err != nil {
		return err
	}
	defer release()
	return fsutil.WriteFileAtomically(abs, content, 0o644)
}

// prependFrontmatterLocked prepends a fenced frontmatter block to a file under a
// per-file lock held across the read and write, so a no-frontmatter adopt is a
// consistent read-modify-write like the merge path.
func prependFrontmatterLocked(ctx context.Context, abs string, fencedBlock []byte) error {
	release, err := fsutil.AcquireFileLock(ctx, abs+".lock")
	if err != nil {
		return err
	}
	defer release()
	content, err := os.ReadFile(abs)
	if err != nil {
		return camperrors.Wrapf(err, "read %s", abs)
	}
	return fsutil.WriteFileAtomically(abs, append(fencedBlock, content...), 0o644)
}

// mergeForeignTags takes ownership of a file's existing tags key: it normalizes
// each conforming foreign value through the tag pipeline and unions those with
// campTags, dropping (and returning) any value that is not a valid tag after
// normalization or is a non-string scalar. This avoids silent data loss under
// --force while keeping camp as the owner of the tags key.
func mergeForeignTags(content []byte, campTags []string) (merged, dropped []string) {
	foreignStrings, nonConforming := wkitem.FrontmatterSequenceStrings(content, "tags")
	dropped = append(dropped, nonConforming...)
	var conforming []string
	for _, fv := range foreignStrings {
		nv := normalizeTag(fv)
		if wkitem.ValidTag(nv) {
			conforming = append(conforming, nv)
		} else {
			dropped = append(dropped, fv)
		}
	}
	return unionStrings(conforming, campTags), dropped
}

// warnReflow prints a one-line note when the existing frontmatter block uses a
// non-2-space indent that a re-encode will reflow, so the byte-for-byte
// deviation is never silent.
func warnReflow(cmd *cobra.Command, content []byte) {
	if w, ok := wkitem.FrontmatterMinIndent(content); ok && w != 2 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"note: existing frontmatter used %d-space indentation; reformatted to 2-space\n", w)
	}
}

func unionStrings(existing, add []string) []string {
	seen := make(map[string]bool, len(existing)+len(add))
	out := make([]string, 0, len(existing)+len(add))
	for _, s := range existing {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range add {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
