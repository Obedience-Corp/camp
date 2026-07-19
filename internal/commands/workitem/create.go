package workitem

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

func newCreateCommand() *cobra.Command {
	var typeFlag, title, idOverride, dirOverride, questSelector string
	var jsonOut bool
	var tags []string
	var projects []string
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create workitem tracking metadata",
		Long: `Create tracking metadata for a new workitem (directory + .workitem marker).

This command does NOT create the substantive work scaffold (no design docs,
explore notes, or festival structure). It only:

  1. Creates workflow/<type>/<slug>/ (or --dir/<slug>/)
  2. Writes a .workitem marker (id, type, title, ref, optional quest, optional
     tags, optional related projects)

Agents and humans must still add real content afterward. For explore/design
types, the recommended structured-workflow scaffold is:

  cd workflow/<type>/<slug> && fest create workflow <slug>

For other types (feature, bug, chore, …), no festival scaffold is implied;
populate campaign-governed content under the new directory as needed.

Use "camp workitem adopt" to attach a marker to an existing directory.
Use --json for machine-readable identity. next.command is set only for
explore/design (recommended scaffold); otherwise it is empty/omitted.`,
		Args: jsoncontract.Args(WorkitemCreateJSONVersion, func() bool { return jsonOut }, cobra.ExactArgs(1)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Creates workitems with --json output for automation",
		},
		RunE: jsoncontract.RunE(WorkitemCreateJSONVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return runCreate(ctx, cmd, args[0], typeFlag, title, idOverride, dirOverride, questSelector, tags, projects, jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(WorkitemCreateJSONVersion, func() bool { return jsonOut }))
	cmd.Flags().StringVar(&typeFlag, "type", "feature", "workitem type (feature, bug, chore, or custom)")
	cmd.Flags().StringVar(&title, "title", "", "human-readable title")
	cmd.Flags().StringVar(&idOverride, "id", "", "override the generated id")
	cmd.Flags().StringVar(&dirOverride, "dir", "", "parent dir override (default: workflow/<type>)")
	cmd.Flags().StringVar(&questSelector, "quest", "", questFlagHelp())
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "add a tag (repeatable, normalized to lowercase kebab-case)")
	cmd.Flags().StringArrayVar(&projects, "project", nil, "add a related project path (repeatable, e.g. projects/camp)")
	return cmd
}

func runCreate(ctx context.Context, cmd *cobra.Command, slug, typeFlag, title, idOverride, dirOverride, questSelector string, tags, projects []string, jsonOut bool) error {
	if err := validateSlug(slug); err != nil {
		return err
	}
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

	id, err := generateID(ctx, typeFlag, slug, idOverride, campaignRoot)
	if err != nil {
		return err
	}

	parent := dirOverride
	if parent == "" {
		parent = filepath.Join("workflow", typeFlag)
	}
	if err := validateParentPath(parent); err != nil {
		return err
	}

	ref, err := deriveUniqueRef(ctx, campaignRoot, cfg, id)
	if err != nil {
		return err
	}
	questID := resolveQuestIDForCreate(ctx, cmd, campaignRoot, questSelector)

	target := filepath.Join(campaignRoot, parent, slug)
	// Existing directories still require explicit adopt or manual cleanup. This
	// command only cleans up an empty directory it created in this invocation.
	if _, err := os.Stat(target); err == nil {
		return camperrors.NewValidation("path",
			"target directory already exists: "+target+" — use `camp workitem adopt` to attach metadata to an existing dir", nil)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return camperrors.Wrap(err, "create directory")
	}
	markerWritten := false
	defer func() {
		if !markerWritten {
			_ = os.Remove(target)
		}
	}()

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
	buf, err := yaml.Marshal(&meta)
	if err != nil {
		return camperrors.Wrap(err, "marshal metadata")
	}
	if err := fsutil.WriteFileAtomically(filepath.Join(target, ".workitem"), buf, 0o644); err != nil {
		return err
	}
	markerWritten = true
	rel := filepath.Join(parent, slug)
	invalidateNavigationCache(cmd, campaignRoot)
	appendWorkitemAuditEvent(ctx, cmd, campaignRoot, wkaudit.Event{
		Event: wkaudit.EventCreate,
		ID:    id,
		Ref:   ref,
		Type:  typeFlag,
		Title: title,
		To:    filepath.ToSlash(rel),
	})
	ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindCreated, ledgerkit.Scope{Workitem: ref, Quest: questID},
			ledger.WithWhy(title),
			ledger.WithPayload(map[string]any{"type": typeFlag, "title": title, "path": rel}))
	nextCommand, nextHint, humanNextLine := createNextGuidance(typeFlag, slug, rel)
	if jsonOut {
		payload := struct {
			SchemaVersion string    `json:"schema_version"`
			GeneratedAt   time.Time `json:"generated_at"`
			Workitem      struct {
				ID            string `json:"id"`
				Ref           string `json:"ref"`
				Type          string `json:"type"`
				Title         string `json:"title,omitempty"`
				QuestID       string `json:"quest_id,omitempty"`
				RelativePath  string `json:"relative_path"`
				MarkerVersion string `json:"marker_version"`
			} `json:"workitem"`
			Next struct {
				Command string `json:"command,omitempty"`
				Cwd     string `json:"cwd"`
				Hint    string `json:"hint"`
			} `json:"next"`
		}{SchemaVersion: WorkitemCreateJSONVersion, GeneratedAt: time.Now().UTC()}
		payload.Workitem.ID = id
		payload.Workitem.Ref = ref
		payload.Workitem.Type = typeFlag
		payload.Workitem.Title = title
		payload.Workitem.QuestID = questID
		payload.Workitem.RelativePath = rel
		payload.Workitem.MarkerVersion = wkitem.WorkitemSchemaVersion
		payload.Next.Command = nextCommand
		payload.Next.Cwd = rel
		payload.Next.Hint = nextHint
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}
	questLine := ""
	if questID != "" {
		questLine = fmt.Sprintf("  quest: %s\n", questID)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"created workitem tracking at %s\n  id: %s\n  ref: %s\n  type: %s\n%s  note: directory + .workitem only — not a design/explore/festival scaffold\n%s",
		rel, id, ref, typeFlag, questLine, humanNextLine)
	return nil
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

// createNextGuidance returns JSON next.command / next.hint and the human
// stdout next line (including trailing newline, or empty when omitted).
// explore/design get a recommended fest scaffold; other types get tracking-only
// guidance with no agent-executable command.
func createNextGuidance(typeFlag, slug, rel string) (command, hint, humanNextLine string) {
	if recommendsWorkflowScaffold(typeFlag) {
		command = "fest create workflow " + slug
		hint = "tracking only: marker created; recommended next: cd " + rel + " && fest create workflow " + slug
		humanNextLine = "  recommended next: cd " + rel + " && fest create workflow " + slug + "\n"
		return command, hint, humanNextLine
	}
	hint = "tracking only: marker created; add content under " + rel + " as needed (no festival scaffold implied)"
	return "", hint, ""
}

func validateSlug(slug string) error {
	return pathutil.ValidateSegment("slug", slug)
}

func validateParentPath(parent string) error {
	clean := filepath.Clean(parent)
	if filepath.IsAbs(clean) {
		return camperrors.NewValidation("dir", "parent dir must be relative to campaign root", nil)
	}
	if strings.HasPrefix(clean, "..") {
		return camperrors.NewValidation("dir", "parent dir must not escape campaign root", nil)
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
