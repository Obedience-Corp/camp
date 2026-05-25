package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/quest"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

func newLinkCommand() *cobra.Command {
	var (
		project, festival, worktree string
		useCwd, replace             bool
		allowMissing, jsonOut       bool
		role                        string
	)
	cmd := &cobra.Command{
		Use:   "link <selector> [path]",
		Short: "Attach a workitem to a project, festival, worktree, or campaign path",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicitPath := ""
			if len(args) == 2 {
				explicitPath = args[1]
			}
			return runLink(cmd.Context(), cmd, linkOptions{
				Selector:     args[0],
				ExplicitPath: explicitPath,
				Project:      project,
				Festival:     festival,
				Worktree:     worktree,
				UseCwd:       useCwd,
				Role:         role,
				Replace:      replace,
				AllowMissing: allowMissing,
				JSON:         jsonOut,
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project name (matches projects/<name>)")
	cmd.Flags().StringVar(&festival, "festival", "", "festival id or relative path under festivals/")
	cmd.Flags().StringVar(&worktree, "worktree", "", "worktree relative path under projects/worktrees/")
	cmd.Flags().BoolVar(&useCwd, "cwd", false, "use current working directory as the scope")
	cmd.Flags().StringVar(&role, "role", string(links.RolePrimary), "primary | related | blocked_by | supersedes")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an existing primary link on the same scope")
	cmd.Flags().BoolVar(&allowMissing, "allow-missing", false, "allow the workitem and scope target to not exist (migrations)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

type linkOptions struct {
	Selector     string
	ExplicitPath string
	Project      string
	Festival     string
	Worktree     string
	UseCwd       bool
	Role         string
	Replace      bool
	AllowMissing bool
	JSON         bool
}

func runLink(ctx context.Context, cmd *cobra.Command, opts linkOptions) error {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	if opts.Role == "" {
		opts.Role = string(links.RolePrimary)
	}
	role := links.Role(opts.Role)
	if !isValidRole(role) {
		return camperrors.NewValidation("role",
			"invalid role "+opts.Role+" (expected primary|related|blocked_by|supersedes)", nil)
	}

	wi, err := resolveSelector(ctx, root, opts.Selector, opts.AllowMissing)
	if err != nil {
		return err
	}

	scope, err := resolveLinkScope(root, opts)
	if err != nil {
		return err
	}

	if !opts.AllowMissing {
		if err := quest.ValidateLinkPath(root, scope.Path); err != nil {
			return camperrors.Wrap(err, "scope path")
		}
	}

	registry, err := links.Load(ctx, root)
	if err != nil {
		return err
	}

	link := links.Link{
		ID:          generateLinkID(),
		WorkitemID:  workitemIDForLink(wi, opts),
		WorkitemKey: workitemKeyForLink(wi),
		Scope:       *scope,
		Role:        role,
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		CreatedBy:   "camp_workitem_link",
	}
	if err := registry.AddLink(link, opts.Replace); err != nil {
		return err
	}

	knownIDs := workitemIDSetFromMaybeNil(wi, opts.AllowMissing)
	if errs := links.Validate(ctx, registry, links.ValidateOptions{
		CampaignRoot: root,
		WorkitemIDs:  knownIDs,
		AllowMissing: opts.AllowMissing,
		Now:          link.CreatedAt,
	}); len(errs) > 0 {
		return camperrors.NewValidation(errs[0].Field, errs[0].Message, nil)
	}

	if err := links.Save(ctx, root, registry); err != nil {
		return err
	}

	if opts.JSON {
		return emitLinkJSON(cmd.OutOrStdout(), link)
	}
	return emitLinkHuman(cmd.OutOrStdout(), link)
}

func resolveSelector(ctx context.Context, root, query string, allowMissing bool) (*wkitem.WorkItem, error) {
	if allowMissing && (strings.HasPrefix(query, "lnk_") == false) {
		// Even with --allow-missing, try the resolver first; if it fails,
		// synthesize a minimal WorkItem so the link can still be saved.
		if wi, err := selector.Resolve(ctx, root, query, selector.ResolveOptions{}); err == nil {
			return wi, nil
		}
		return &wkitem.WorkItem{
			Key:      query,
			StableID: query,
		}, nil
	}
	return selector.Resolve(ctx, root, query, selector.ResolveOptions{})
}

func resolveLinkScope(root string, opts linkOptions) (*links.LinkScope, error) {
	chosen := 0
	for _, v := range []string{opts.Project, opts.Festival, opts.Worktree, opts.ExplicitPath} {
		if v != "" {
			chosen++
		}
	}
	if opts.UseCwd {
		chosen++
	}
	if chosen == 0 {
		return nil, camperrors.NewValidation("scope",
			"must specify one of --project, --festival, --worktree, --cwd, or a path argument", nil)
	}
	if chosen > 1 {
		return nil, camperrors.NewValidation("scope",
			"--project, --festival, --worktree, --cwd, and the path argument are mutually exclusive", nil)
	}

	switch {
	case opts.Project != "":
		return &links.LinkScope{Kind: links.ScopeProject, Path: filepath.ToSlash(filepath.Join("projects", opts.Project))}, nil
	case opts.Festival != "":
		path := opts.Festival
		if !strings.HasPrefix(path, "festivals/") {
			path = filepath.ToSlash(filepath.Join("festivals", "active", path))
		}
		return &links.LinkScope{Kind: links.ScopeFestival, Path: path}, nil
	case opts.Worktree != "":
		path := opts.Worktree
		if !strings.HasPrefix(path, "projects/worktrees/") {
			path = filepath.ToSlash(filepath.Join("projects", "worktrees", opts.Worktree))
		}
		return &links.LinkScope{Kind: links.ScopeWorktree, Path: path}, nil
	case opts.UseCwd:
		cwd, err := os.Getwd()
		if err != nil {
			return nil, camperrors.Wrap(err, "get cwd")
		}
		rel, err := filepath.Rel(root, cwd)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, camperrors.NewValidation("scope",
				"current directory is outside the campaign root", nil)
		}
		rel = filepath.ToSlash(rel)
		return &links.LinkScope{Kind: inferScopeKind(rel), Path: rel}, nil
	default: // explicit path
		path := filepath.ToSlash(opts.ExplicitPath)
		return &links.LinkScope{Kind: inferScopeKind(path), Path: path}, nil
	}
}

func inferScopeKind(rel string) links.ScopeKind {
	switch {
	case strings.HasPrefix(rel, "projects/worktrees/"):
		return links.ScopeWorktree
	case strings.HasPrefix(rel, "projects/"):
		return links.ScopeProject
	case strings.HasPrefix(rel, "festivals/"):
		return links.ScopeFestival
	default:
		return links.ScopeCampaignPath
	}
}

func workitemIDForLink(wi *wkitem.WorkItem, opts linkOptions) string {
	if wi.StableID != "" {
		return wi.StableID
	}
	if opts.AllowMissing {
		return opts.Selector
	}
	return wi.Key
}

func workitemKeyForLink(wi *wkitem.WorkItem) string {
	return wi.Key
}

func workitemIDSetFromMaybeNil(wi *wkitem.WorkItem, allowMissing bool) map[string]struct{} {
	if allowMissing {
		return nil
	}
	if wi == nil || wi.StableID == "" {
		return nil
	}
	return map[string]struct{}{wi.StableID: {}}
}

func emitLinkHuman(w io.Writer, link links.Link) error {
	_, err := fmt.Fprintf(w,
		"linked %s -> %s:%s (role %s, id %s)\n",
		link.WorkitemID, link.Scope.Kind, link.Scope.Path, link.Role, link.ID)
	return err
}

func emitLinkJSON(w io.Writer, link links.Link) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string     `json:"schema_version"`
		GeneratedAt   time.Time  `json:"generated_at"`
		Link          links.Link `json:"link"`
	}{
		SchemaVersion: links.LinksSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Link:          link,
	})
}

func generateLinkID() string {
	// Local-only ID generator: yyyymmdd + 6 hex bytes of randomness.
	// crypto/rand keeps the per-day collision domain wide.
	var b [3]byte
	if _, err := readRand(b[:]); err != nil {
		// In the unlikely case of rand failure, fall back to a time-derived
		// suffix so we still produce a valid ID rather than abort the link.
		return fmt.Sprintf("lnk_%s_%06x",
			time.Now().UTC().Format("20060102"),
			time.Now().UnixNano()&0xffffff)
	}
	return fmt.Sprintf("lnk_%s_%02x%02x%02x",
		time.Now().UTC().Format("20060102"), b[0], b[1], b[2])
}

func isValidRole(r links.Role) bool {
	for _, valid := range links.ValidRoles {
		if r == valid {
			return true
		}
	}
	return false
}
