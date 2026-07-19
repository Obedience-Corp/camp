package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/quest"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
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
		Short: "Create a workitem link",
		Long: `Attach a workitem to a project, festival, worktree, or campaign path.

Links are stored in .campaign/workitems/links.yaml and connect a .workitem
identity to an explicit scope for planning, execution, and lookup. Pass a
workitem selector plus a path, or use --project, --festival, --worktree, or
--cwd to derive the scope. Use --json for machine-readable link output.

A primary worktree link is how design/explore workitems under workflow/ get
into camp p commit tags: when you commit from that worktree, the resolver
matches the link and stamps WI-<ref> on the subject.

Examples:
  camp workitem link WI-2a7950 --worktree fest/fest-list-watch
  camp workitem link workflow/design/fest-list-watch --worktree projects/worktrees/fest/fest-list-watch
  camp workitem link WI-2a7950 projects/worktrees/fest/fest-list-watch
  # Or at create time:
  camp project worktree add fest-list-watch --project fest --workitem WI-2a7950`,
		Args: jsoncontract.Args(links.LinksSchemaVersion, func() bool { return jsonOut }, cobra.RangeArgs(1, 2)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Links workitems to scopes with --json output for automation",
		},
		RunE: jsoncontract.RunE(links.LinksSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
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
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(links.LinksSchemaVersion, func() bool { return jsonOut }))
	cmd.Flags().StringVar(&project, "project", "", "project name (matches projects/<name>)")
	cmd.Flags().StringVar(&festival, "festival", "", "festival id or relative path under festivals/")
	cmd.Flags().StringVar(&worktree, "worktree", "", "worktree path under projects/worktrees/ (project/name or full projects/worktrees/project/name)")
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

	var knownIDs map[string]struct{}
	if !opts.AllowMissing {
		knownIDs, _, err = workitemIDsOnDisk(ctx, root)
		if err != nil {
			return err
		}
	}

	var link links.Link
	err = links.WithLock(ctx, root, func(registry *links.Links) error {
		// Generate the ID inside the lock so the collision retry sees
		// the current registry state, not a stale snapshot.
		id, idErr := generateLinkID(registry)
		if idErr != nil {
			return idErr
		}
		link = links.Link{
			ID:          id,
			WorkitemID:  workitemIDForLink(wi, opts),
			WorkitemKey: workitemKeyForLink(wi),
			Scope:       *scope,
			Role:        role,
			CreatedAt:   time.Now().UTC().Truncate(time.Second),
			CreatedBy:   defaultCreatedBy(),
		}
		if err := registry.AddLink(link, opts.Replace); err != nil {
			return err
		}
		if errs := links.Validate(ctx, registry, links.ValidateOptions{
			CampaignRoot: root,
			WorkitemIDs:  knownIDs,
			AllowMissing: opts.AllowMissing,
			Now:          link.CreatedAt,
		}); len(errs) > 0 {
			return camperrors.NewValidation(errs[0].Field, errs[0].Message, nil)
		}
		return nil
	})
	if err != nil {
		return err
	}

	ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr())).
		Emit(ctx, ledgerkit.KindCreated, ledgerkit.Scope{Workitem: link.WorkitemID},
			ledger.WithPayload(map[string]any{
				"link_id": link.ID, "role": string(link.Role),
				"scope_kind": string(link.Scope.Kind), "scope_path": link.Scope.Path,
			}))

	if opts.JSON {
		return emitLinkJSON(cmd.OutOrStdout(), link)
	}
	return emitLinkHuman(cmd.OutOrStdout(), link)
}

var createdBySanitizer = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func defaultCreatedBy() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		if sanitized := sanitizeCreatedBy(u.Username); sanitized != "" {
			return sanitized
		}
	}
	return "camp_workitem_link"
}

func sanitizeCreatedBy(value string) string {
	sanitized := createdBySanitizer.ReplaceAllString(value, "-")
	sanitized = strings.Trim(sanitized, "-")
	if len(sanitized) > 64 {
		sanitized = sanitized[:64]
	}
	return sanitized
}

func resolveSelector(ctx context.Context, root, query string, allowMissing bool) (*wkitem.WorkItem, error) {
	if allowMissing && !strings.HasPrefix(query, "lnk_") {
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
	if wi.WorkflowType == wkitem.WorkflowTypeFestival && wi.SourceID != "" {
		return wi.SourceID
	}
	if opts.AllowMissing {
		return opts.Selector
	}
	return wi.Key
}

func workitemKeyForLink(wi *wkitem.WorkItem) string {
	return wi.Key
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

// generateLinkID returns a fresh lnk_YYYYMMDD_<6 hex> ID that does not
// collide with any existing entry in registry. It retries up to 32 times
// against crypto/rand and returns an error rather than falling back to a
// wall-clock suffix because a non-unique ID corrupts the registry's primary
// key invariant.
func generateLinkID(registry *links.Links) (string, error) {
	existing := make(map[string]struct{}, len(registry.Links))
	for _, l := range registry.Links {
		existing[l.ID] = struct{}{}
	}
	const maxAttempts = 32
	for i := 0; i < maxAttempts; i++ {
		var b [3]byte
		if _, err := readRand(b[:]); err != nil {
			return "", camperrors.Wrap(err, "generate link id: read random bytes")
		}
		candidate := fmt.Sprintf("lnk_%s_%02x%02x%02x",
			time.Now().UTC().Format("20060102"), b[0], b[1], b[2])
		if _, clash := existing[candidate]; !clash {
			return candidate, nil
		}
	}
	return "", camperrors.New(fmt.Sprintf("generate link id: %d-attempt collision retry exhausted", maxAttempts))
}

func isValidRole(r links.Role) bool {
	for _, valid := range links.ValidRoles {
		if r == valid {
			return true
		}
	}
	return false
}
