package workitem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

func newUnlinkCommand() *cobra.Command {
	var (
		id, project, festival, worktree string
		all, jsonOut                    bool
	)
	cmd := &cobra.Command{
		Use:   "unlink [selector] [path]",
		Short: "Remove workitem links",
		Long: `Remove workitem links from the campaign link registry.

The command updates .campaign/workitems/links.yaml by link id, workitem
selector, explicit path, or scope filter. Use --all when a selector matches
multiple links and every match should be removed. Use --json for
machine-readable details about the removed links.`,
		Args: jsoncontract.Args(links.LinksSchemaVersion, func() bool { return jsonOut }, cobra.RangeArgs(0, 2)),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Removes workitem links with --json output for automation",
		},
		RunE: jsoncontract.RunE(links.LinksSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			selectorArg := ""
			path := ""
			if len(args) >= 1 {
				selectorArg = args[0]
			}
			if len(args) == 2 {
				path = args[1]
			}
			return runUnlink(cmd.Context(), cmd, unlinkOptions{
				ID:           id,
				Selector:     selectorArg,
				ExplicitPath: path,
				Project:      project,
				Festival:     festival,
				Worktree:     worktree,
				All:          all,
				JSON:         jsonOut,
			})
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(links.LinksSchemaVersion, func() bool { return jsonOut }))
	cmd.Flags().StringVar(&id, "id", "", "remove the link with this lnk_ id")
	cmd.Flags().StringVar(&project, "project", "", "project scope filter")
	cmd.Flags().StringVar(&festival, "festival", "", "festival scope filter")
	cmd.Flags().StringVar(&worktree, "worktree", "", "worktree scope filter")
	cmd.Flags().BoolVar(&all, "all", false, "remove every link matching the selector")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

type unlinkOptions struct {
	ID           string
	Selector     string
	ExplicitPath string
	Project      string
	Festival     string
	Worktree     string
	All          bool
	JSON         bool
}

func runUnlink(ctx context.Context, cmd *cobra.Command, opts unlinkOptions) error {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}
	removed := []links.Link{}
	err = links.WithLock(ctx, root, func(registry *links.Links) error {
		switch {
		case opts.ID != "":
			link, ok := registry.FindByID(opts.ID)
			if !ok {
				return camperrors.NewValidation("id", "no link with id "+opts.ID, nil)
			}
			removed = append(removed, *link)
			registry.RemoveLinkByID(opts.ID)

		case opts.Selector != "":
			wi, err := resolveSelector(ctx, root, opts.Selector, false)
			if err != nil {
				return err
			}
			matches := matchUnlinkCandidates(registry, wi.StableID, opts)
			if len(matches) == 0 {
				return camperrors.NewValidation("selector",
					"no links matched selector "+opts.Selector, nil)
			}
			if len(matches) > 1 && !opts.All {
				return camperrors.NewValidation("selector",
					fmt.Sprintf("selector matched %d links; pass --all to remove every match or --id to pick one", len(matches)), nil)
			}
			for _, link := range matches {
				registry.RemoveLinkByID(link.ID)
				removed = append(removed, link)
			}

		default:
			return camperrors.NewValidation("selector",
				"must provide --id or a selector to unlink", nil)
		}
		return nil
	})
	if err != nil {
		return err
	}

	emitter := ledger.NewFromRoot(ctx, root, ledger.WarnTo(cmd.ErrOrStderr()))
	for _, link := range removed {
		emitter.Emit(ctx, ledgerkit.KindTransitioned, ledgerkit.Scope{Workitem: link.WorkitemID},
			ledger.WithPayload(map[string]any{"op": "unlink", "link_id": link.ID, "role": string(link.Role)}))
	}

	if opts.JSON {
		return emitUnlinkJSON(cmd.OutOrStdout(), removed)
	}
	return emitUnlinkHuman(cmd.OutOrStdout(), removed)
}

func matchUnlinkCandidates(registry *links.Links, workitemID string, opts unlinkOptions) []links.Link {
	scopeFilter, useScope := unlinkScopeFilter(opts)
	var out []links.Link
	for _, link := range registry.Links {
		if link.WorkitemID != workitemID {
			continue
		}
		if useScope {
			if link.Scope.Kind != scopeFilter.Kind || link.Scope.Path != scopeFilter.Path {
				continue
			}
		}
		out = append(out, link)
	}
	return out
}

func unlinkScopeFilter(opts unlinkOptions) (links.LinkScope, bool) {
	switch {
	case opts.Project != "":
		return links.LinkScope{Kind: links.ScopeProject, Path: "projects/" + opts.Project}, true
	case opts.Festival != "":
		path := opts.Festival
		if !strings.HasPrefix(path, "festivals/") {
			path = "festivals/active/" + path
		}
		return links.LinkScope{Kind: links.ScopeFestival, Path: path}, true
	case opts.Worktree != "":
		path := opts.Worktree
		if !strings.HasPrefix(path, "projects/worktrees/") {
			path = "projects/worktrees/" + opts.Worktree
		}
		return links.LinkScope{Kind: links.ScopeWorktree, Path: path}, true
	case opts.ExplicitPath != "":
		return links.LinkScope{Kind: inferScopeKind(opts.ExplicitPath), Path: opts.ExplicitPath}, true
	}
	return links.LinkScope{}, false
}

func emitUnlinkHuman(w io.Writer, removed []links.Link) error {
	if len(removed) == 0 {
		_, err := fmt.Fprintln(w, "no links removed")
		return err
	}
	if _, err := fmt.Fprintf(w, "removed %d link(s)\n", len(removed)); err != nil {
		return err
	}
	for _, link := range removed {
		if _, err := fmt.Fprintf(w, "  %s  %s -> %s:%s\n",
			link.ID, link.WorkitemID, link.Scope.Kind, link.Scope.Path); err != nil {
			return err
		}
	}
	return nil
}

func emitUnlinkJSON(w io.Writer, removed []links.Link) error {
	if removed == nil {
		removed = []links.Link{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string       `json:"schema_version"`
		GeneratedAt   time.Time    `json:"generated_at"`
		Removed       []links.Link `json:"removed"`
	}{
		SchemaVersion: links.LinksSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Removed:       removed,
	})
}
