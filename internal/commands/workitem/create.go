package workitem

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

func newCreateCommand() *cobra.Command {
	var typeFlag, title, idOverride, dirOverride, questSelector, fileFlag string
	var jsonOut bool
	var tags []string
	var projects []string
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create workitem tracking metadata",
		Long: `Create tracking metadata for a new workitem (directory + .workitem marker).

Without --type, the type comes from where the workitem is created. Run from
anywhere under workflow/<type>/, including inside another workitem, and the
new item gets that type and is created at workflow/<type>/<slug>/, a sibling
of the items already there. --dir workflow/<type>[/...] and
--file workflow/<type>/<name>.md infer the type the same way. Everywhere else
the type defaults to feature. An explicit --type always wins.

This command does NOT create the substantive work scaffold (no design docs,
explore notes, or festival structure). It only:

  1. Creates workflow/<type>/<slug>/ (or --dir/<slug>/)
  2. Writes a .workitem marker (id, type, title, ref, optional quest, optional
     tags, optional related projects)

Agents and humans must still add real content afterward. For explore/design
types, the recommended structured-workflow scaffold is:

  cd workflow/<type>/<slug> && fest create workflow <slug>

For other types (feature, bug, chore, …), no festival scaffold is implied;
populate camp-governed content under the new directory as needed.

Use "camp workitem adopt" to attach a marker to an existing directory.
Use --json for machine-readable identity. next.command is set only for
explore/design (recommended scaffold); otherwise it is empty/omitted.`,
		Args: jsoncontract.Args(WorkitemCreateJSONVersion, func() bool { return jsonOut }, cobra.MaximumNArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Creates workitems with --json output for automation",
		},
		RunE: jsoncontract.RunE(WorkitemCreateJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			typeExplicit := cmd.Flags().Changed("type")
			if fileFlag != "" {
				if len(args) > 0 {
					return camperrors.NewValidation("args", "provide either a slug argument or --file, not both", nil)
				}
				return runCreateFile(ctx, cmd, fileFlag, typeFlag, typeExplicit, title, idOverride, questSelector, tags, projects, jsonOut)
			}
			if len(args) != 1 {
				return camperrors.NewValidation("args", "create requires a slug argument or --file <path>", nil)
			}
			return runCreate(ctx, cmd, args[0], typeFlag, typeExplicit, title, idOverride, dirOverride, questSelector, tags, projects, jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemCreateJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().StringVar(&typeFlag, "type", "feature", "workitem type (feature, bug, chore, or custom); when omitted, inferred from a workflow/<type>/ cwd, --dir, or --file")
	cmd.Flags().StringVar(&title, "title", "", "human-readable title")
	cmd.Flags().StringVar(&idOverride, "id", "", "override the generated id")
	cmd.Flags().StringVar(&dirOverride, "dir", "", "parent dir override (default: workflow/<type>)")
	cmd.Flags().StringVar(&questSelector, "quest", "", questFlagHelp())
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "add a tag (repeatable, normalized to lowercase kebab-case)")
	cmd.Flags().StringArrayVar(&projects, "project", nil, "add a related project path (repeatable, e.g. projects/camp)")
	cmd.Flags().StringVar(&fileFlag, "file", "", "create a new markdown file with kind: workitem frontmatter instead of a directory workitem")
	return cmd
}

// runCreateFile mints a new markdown file with a kind: workitem frontmatter
// block and a minimal heading body, reusing the frontmatter construction of the
// no-existing-frontmatter adopt branch.
func runCreateFile(ctx context.Context, cmd *cobra.Command, filePath, typeFlag string, typeExplicit bool, title, idOverride, questSelector string, tags, projects []string, jsonOut bool) error {
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
		return camperrors.Wrap(err, "not in a camp directory")
	}

	rel := filePath
	if filepath.IsAbs(filePath) {
		rel, err = filepath.Rel(campaignRoot, filePath)
		if err != nil {
			return camperrors.Wrap(err, "resolve file relative to camp root")
		}
	}
	if err := validateParentPath(rel); err != nil {
		return err
	}
	if !strings.HasSuffix(rel, ".md") {
		return camperrors.NewValidation("file", "create --file target must be a .md file, got "+rel, nil)
	}
	abs := filepath.Join(campaignRoot, rel)
	if _, statErr := os.Stat(abs); statErr == nil {
		return camperrors.NewValidation("path",
			"target file already exists: "+rel+" — use `camp workitem adopt --file` to stamp an existing file", nil)
	}

	placement := planCreateFile(typeFlag, typeExplicit, rel)
	typ := placement.Type
	slug := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	id, err := generateID(ctx, typ, slug, idOverride, campaignRoot)
	if err != nil {
		return err
	}
	ref, err := deriveUniqueRef(ctx, campaignRoot, cfg, id)
	if err != nil {
		return err
	}
	questID := resolveQuestIDForCreate(ctx, cmd, campaignRoot, questSelector)

	titleText := title
	if titleText == "" {
		titleText = slug
	}
	meta := wkitem.Metadata{
		Version:  wkitem.WorkitemSchemaVersion,
		Kind:     "workitem",
		ID:       id,
		Type:     typ,
		Title:    titleText,
		Ref:      ref,
		QuestID:  questID,
		Tags:     normalizedTags,
		Projects: normalizedProjects,
	}
	fmBlock, err := wkitem.MarshalMetadataFrontmatter(&meta)
	if err != nil {
		return err
	}
	content := append(wkitem.FenceFrontmatter(fmBlock), []byte("# "+titleText+"\n")...)

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return camperrors.Wrap(err, "create parent directory")
	}
	if err := writeFileLocked(ctx, abs, content); err != nil {
		return err
	}

	invalidateNavigationCache(cmd, campaignRoot)
	appendWorkitemAuditEvent(ctx, cmd, campaignRoot, wkaudit.Event{
		Event: wkaudit.EventCreate,
		ID:    id,
		Ref:   ref,
		Type:  typ,
		Title: titleText,
		To:    filepath.ToSlash(rel),
	})
	ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindCreated, ledgerkit.Scope{Workitem: ref, Quest: questID},
			ledger.WithWhy(titleText),
			ledger.WithPayload(map[string]any{"type": typ, "title": titleText, "path": rel, "file": true}))

	if jsonOut {
		payload := struct {
			SchemaVersion string    `json:"schema_version"`
			GeneratedAt   time.Time `json:"generated_at"`
			Workitem      struct {
				ID            string   `json:"id"`
				Ref           string   `json:"ref"`
				Type          string   `json:"type"`
				Title         string   `json:"title,omitempty"`
				RelativePath  string   `json:"relative_path"`
				ItemKind      string   `json:"item_kind"`
				MarkerVersion string   `json:"marker_version"`
				Tags          []string `json:"tags"`
				Projects      []string `json:"projects"`
			} `json:"workitem"`
		}{SchemaVersion: WorkitemCreateJSONVersion, GeneratedAt: time.Now().UTC()}
		payload.Workitem.ID = id
		payload.Workitem.Ref = ref
		payload.Workitem.Type = typ
		payload.Workitem.Title = titleText
		payload.Workitem.RelativePath = rel
		payload.Workitem.ItemKind = "file"
		payload.Workitem.MarkerVersion = wkitem.WorkitemSchemaVersion
		payload.Workitem.Tags = jsonStringSlice(normalizedTags)
		payload.Workitem.Projects = jsonStringSlice(normalizedProjects)
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	return writeCreateSummary(cmd.OutOrStdout(), createSummary{
		Type:         typ,
		TypeExplicit: typeExplicit,
		TypeFrom:     placement.From,
		Slug:         slug,
		Path:         filepath.ToSlash(rel),
		ID:           id,
		Ref:          ref,
		QuestID:      questID,
	})
}

func runCreate(ctx context.Context, cmd *cobra.Command, slug, typeFlag string, typeExplicit bool, title, idOverride, dirOverride, questSelector string, tags, projects []string, jsonOut bool) error {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}
	questID := resolveQuestIDForCreate(ctx, cmd, campaignRoot, questSelector)
	cwdRel, cwdInCamp := campRelativeCwd(campaignRoot)
	placement := planCreateDir(typeFlag, typeExplicit, dirOverride, cwdRel)
	typ := placement.Type

	created, err := CreateWorkitemDir(ctx, campaignRoot, cfg, CreateWorkitemRequest{
		Slug: slug, Type: typ, Title: title, IDOverride: idOverride,
		DirOverride: placement.Parent, QuestID: questID, Tags: tags, Projects: projects,
	})
	if err != nil {
		return err
	}
	id, ref, rel := created.ID, created.Ref, created.RelativePath

	invalidateNavigationCache(cmd, campaignRoot)
	appendWorkitemAuditEvent(ctx, cmd, campaignRoot, wkaudit.Event{
		Event: wkaudit.EventCreate,
		ID:    id,
		Ref:   ref,
		Type:  typ,
		Title: title,
		To:    filepath.ToSlash(rel),
	})
	ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindCreated, ledgerkit.Scope{Workitem: ref, Quest: questID},
			ledger.WithWhy(title),
			ledger.WithPayload(map[string]any{"type": typ, "title": title, "path": rel}))
	nextCommand, nextHint := createNextGuidance(typ, slug, rel)
	if jsonOut {
		payload := struct {
			SchemaVersion string    `json:"schema_version"`
			GeneratedAt   time.Time `json:"generated_at"`
			Workitem      struct {
				ID            string   `json:"id"`
				Ref           string   `json:"ref"`
				Type          string   `json:"type"`
				Title         string   `json:"title,omitempty"`
				QuestID       string   `json:"quest_id,omitempty"`
				RelativePath  string   `json:"relative_path"`
				MarkerVersion string   `json:"marker_version"`
				Tags          []string `json:"tags"`
				Projects      []string `json:"projects"`
			} `json:"workitem"`
			Next struct {
				Command string `json:"command,omitempty"`
				Cwd     string `json:"cwd"`
				Hint    string `json:"hint"`
			} `json:"next"`
		}{SchemaVersion: WorkitemCreateJSONVersion, GeneratedAt: time.Now().UTC()}
		payload.Workitem.ID = id
		payload.Workitem.Ref = ref
		payload.Workitem.Type = typ
		payload.Workitem.Title = title
		payload.Workitem.QuestID = questID
		payload.Workitem.RelativePath = rel
		payload.Workitem.MarkerVersion = wkitem.WorkitemSchemaVersion
		payload.Workitem.Tags = jsonStringSlice(created.Tags)
		payload.Workitem.Projects = jsonStringSlice(created.Projects)
		payload.Next.Command = nextCommand
		payload.Next.Cwd = rel
		payload.Next.Hint = nextHint
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}
	humanNext := ""
	if nextCommand != "" {
		humanNext = "cd " + cdTargetFromCwd(cwdRel, cwdInCamp, rel) + " && " + nextCommand
	}
	return writeCreateSummary(cmd.OutOrStdout(), createSummary{
		Type:         typ,
		TypeExplicit: typeExplicit,
		TypeFrom:     placement.From,
		Slug:         slug,
		Path:         rel,
		ID:           id,
		Ref:          ref,
		QuestID:      questID,
		Next:         humanNext,
		Hint:         createTrackingOnlyHint,
	})
}

func jsonStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// recommendsWorkflowScaffold reports whether fest create workflow is the
// recommended structured next step for this workitem type (explore/design).
func recommendsWorkflowScaffold(typeFlag string) bool {
	switch strings.ToLower(typeFlag) {
	case "explore", "design":
		return true
	default:
		return false
	}
}

// createNextGuidance returns JSON next.command / next.hint. explore/design get
// a recommended fest scaffold; other types get tracking-only guidance with no
// agent-executable command.
func createNextGuidance(typeFlag, slug, rel string) (command, hint string) {
	if recommendsWorkflowScaffold(typeFlag) {
		command = "fest create workflow " + slug
		hint = "tracking only: marker created; recommended next: cd " + rel + " && fest create workflow " + slug
		return command, hint
	}
	hint = "tracking only: marker created; add content under " + rel + " as needed (no festival scaffold implied)"
	return "", hint
}

func validateSlug(slug string) error {
	return pathutil.ValidateSegment("slug", slug)
}

func validateParentPath(parent string) error {
	clean := filepath.Clean(parent)
	if filepath.IsAbs(clean) {
		return camperrors.NewValidation("dir", "parent dir must be relative to camp root", nil)
	}
	if strings.HasPrefix(clean, "..") {
		return camperrors.NewValidation("dir", "parent dir must not escape camp root", nil)
	}
	return nil
}

func generateID(ctx context.Context, typeStr, slug, override, campaignRoot string) (string, error) {
	if override != "" {
		if err := validateSlug(override); err != nil {
			return "", camperrors.NewValidation("id",
				"invalid id override "+override+": ids follow the same path-safe slug contract as workitem names (no '/', '\\', whitespace, or control chars; no leading '.' or '-'; max 80 chars)", nil)
		}
		collides, err := idCollides(ctx, campaignRoot, override)
		if err != nil {
			return "", camperrors.Wrap(err, "scan for id collision")
		}
		if collides {
			return "", camperrors.NewValidation("id",
				"id override "+override+" collides with an existing .workitem; choose a different id", nil)
		}
		return override, nil
	}
	base := typeStr + "-" + slug + "-" + time.Now().UTC().Format("2006-01-02")
	collides, err := idCollides(ctx, campaignRoot, base)
	if err != nil {
		return "", camperrors.Wrap(err, "scan for id collision")
	}
	if !collides {
		return base, nil
	}
	for i := 0; i < 32; i++ {
		var b [3]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", camperrors.Wrap(err, "generate id suffix")
		}
		candidate := base + "-" + hex.EncodeToString(b[:])
		collides, err := idCollides(ctx, campaignRoot, candidate)
		if err != nil {
			return "", camperrors.Wrap(err, "scan for id collision")
		}
		if !collides {
			return candidate, nil
		}
	}
	return "", camperrors.NewValidation("id", "could not generate a unique id after 32 attempts", nil)
}

func idCollides(ctx context.Context, campaignRoot, id string) (bool, error) {
	if campaignRoot == "" {
		return false, nil
	}
	root := filepath.Join(campaignRoot, "workflow")
	collision := false
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && path == root {
				return filepath.SkipAll
			}
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() || filepath.Base(path) != ".workitem" {
			return nil
		}
		raw, rErr := os.ReadFile(path)
		if rErr != nil {
			return rErr
		}
		var m wkitem.Metadata
		if uErr := yaml.Unmarshal(raw, &m); uErr != nil {
			return nil
		}
		if m.ID == id {
			collision = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return false, walkErr
	}
	return collision, nil
}
