//go:build dev

// Package event implements the `camp event` command group, whose `add`
// subcommand is the explicit campaign-ledger capture primitive: a one-command
// way to record an action that never touches git (media production and other
// out-of-band work), with cwd scope inference and typed evidence (D005).
package event

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

// Cmd is the `camp event` command group.
var Cmd = &cobra.Command{
	Use:   "event",
	Short: "Record and inspect campaign ledger events",
	Long: `Record and inspect campaign event-ledger entries.

The ledger is the append-only trail of high-intent actions across a campaign.
Most events are captured automatically by state-changing camp/fest commands;
'camp event add' is the explicit escape hatch for actions that never touch git.`,
}

// validKinds is the D001 closed-ish kind set the add command accepts.
var validKinds = []ledgerkit.Kind{
	ledgerkit.KindCreated, ledgerkit.KindTransitioned, ledgerkit.KindCompleted,
	ledgerkit.KindDecided, ledgerkit.KindEvidenceAttached, ledgerkit.KindReconciled,
	ledgerkit.KindRepaired,
}

func init() {
	Cmd.AddCommand(newAddCommand())
}

type addOptions struct {
	kind     string
	why      string
	workitem string
	festival string
	quest    string
	evidence []string
	action   string
	jsonOut  bool
}

func newAddCommand() *cobra.Command {
	opts := &addOptions{}
	cmd := &cobra.Command{
		Use:   "add <title>",
		Short: "Record an explicit campaign ledger event",
		Long: `Record an explicit campaign ledger event for an out-of-band action.

Scope is inferred from the current directory (the workitem or festival you are
in); flags override inference. Evidence may be a campaign-relative path, a URL,
or a repo@sha commit reference, and may be repeated.

Examples:
  # A media-production decision, with the produced file as evidence
  camp event add --type decided "chose H.265 for the trailer" \
    --why "smaller files, target players all support it" \
    --evidence renders/trailer_final_v3.mp4

  # A quick note-to-trail from inside a workitem directory
  camp event add --type created "kicked off the color grade pass"`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Explicit ledger capture; flags-only, supports --json for automation",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, args[0], opts)
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.kind, "type", "", "event kind (required): "+kindList())
	f.StringVar(&opts.why, "why", "", "the reason for the action (rendered prominently)")
	f.StringVar(&opts.workitem, "workitem", "", "workitem selector to scope the event (overrides cwd inference)")
	f.StringVar(&opts.festival, "festival", "", "festival id to scope the event (overrides cwd inference)")
	f.StringVar(&opts.quest, "quest", "", "quest id to scope the event")
	f.StringArrayVar(&opts.evidence, "evidence", nil, "evidence ref: <path> | <url> | <repo>@<sha> (repeatable)")
	f.StringVar(&opts.action, "action", "", "join an existing action id (default: a fresh action per invocation)")
	f.BoolVar(&opts.jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runAdd(cmd *cobra.Command, title string, opts *addOptions) error {
	ctx := cmd.Context()

	kind, err := parseKind(opts.kind)
	if err != nil {
		return err
	}

	cfg, campRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	evidence, warnings, err := parseEvidence(campRoot, opts.evidence)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), ui.Warning(w))
	}

	scope := inferScope(ctx, campRoot, opts)

	emitter := ledger.New(ctx, campRoot, cfg.ID, ledger.WarnTo(cmd.ErrOrStderr()))
	emitOpts := []ledger.Option{ledger.WithWhy(opts.why)}
	if len(evidence) > 0 {
		emitOpts = append(emitOpts, ledger.WithEvidence(evidence...))
	}
	if opts.action != "" {
		emitOpts = append(emitOpts, ledger.WithAction(opts.action))
	}
	emitOpts = append(emitOpts, ledger.WithPayload(map[string]any{"title": title}))

	eventID, shard, err := emitter.AddExplicit(ctx, kind, scope, emitOpts...)
	if err != nil {
		return camperrors.Wrap(err, "event not recorded: ledger write failed")
	}
	if eventID == "" {
		return camperrors.New("event not recorded: the campaign ledger could not be written (see warning above)")
	}

	actionID := opts.action
	if actionID == "" {
		actionID = emitter.ActionID()
	}
	shardRel, relErr := filepath.Rel(campRoot, shard)
	if relErr != nil {
		shardRel = shard
	}

	if opts.jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			SchemaVersion string `json:"schema_version"`
			EventID       string `json:"event_id"`
			ActionID      string `json:"action_id"`
			Kind          string `json:"kind"`
			Shard         string `json:"shard"`
		}{"camp-event-add/v1", eventID, actionID, string(kind), shardRel})
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), ui.Success("✓ Event recorded: "+string(kind)+" ("+eventID+")"))
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), ui.Dim("  landed in "+shardRel))
	return nil
}

func parseKind(raw string) (ledgerkit.Kind, error) {
	if raw == "" {
		return "", camperrors.NewValidation("type", "required flag --type not set (valid: "+kindList()+")", nil)
	}
	k := ledgerkit.Kind(strings.ToLower(strings.TrimSpace(raw)))
	for _, valid := range validKinds {
		if k == valid {
			return k, nil
		}
	}
	return "", camperrors.NewValidation("type", "unknown event kind "+raw+" (valid: "+kindList()+")", nil)
}

func kindList() string {
	names := make([]string, len(validKinds))
	for i, k := range validKinds {
		names[i] = string(k)
	}
	return strings.Join(names, ", ")
}

var (
	urlRe       = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
	repoAtShaRe = regexp.MustCompile(`^([A-Za-z0-9._/-]+)@([0-9a-fA-F]{7,40})$`)
)

// parseEvidence classifies each raw evidence ref. Paths are stored
// campaign-relative and their absence is a warning (media files move), not a
// failure; url and repo@sha refs are shape-validated only.
func parseEvidence(campRoot string, raws []string) ([]ledgerkit.Evidence, []string, error) {
	var refs []ledgerkit.Evidence
	var warnings []string
	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		switch {
		case urlRe.MatchString(raw):
			refs = append(refs, ledgerkit.Evidence{Type: ledgerkit.EvidenceURL, URL: raw})
		case repoAtShaRe.MatchString(raw):
			m := repoAtShaRe.FindStringSubmatch(raw)
			refs = append(refs, ledgerkit.Evidence{Type: ledgerkit.EvidenceCommit, Repo: m[1], SHA: m[2]})
		default:
			rel := raw
			abs := raw
			if !filepath.IsAbs(raw) {
				abs = filepath.Join(campRoot, raw)
			}
			if r, err := filepath.Rel(campRoot, abs); err == nil && !strings.HasPrefix(r, "..") {
				rel = filepath.ToSlash(r)
			}
			if _, statErr := os.Stat(abs); statErr != nil {
				warnings = append(warnings, "evidence path does not exist (recorded anyway): "+rel)
			}
			refs = append(refs, ledgerkit.Evidence{Type: ledgerkit.EvidencePath, Path: rel})
		}
	}
	return refs, warnings, nil
}

// inferScope fills the event scope: explicit flags win, otherwise the workitem
// resolver maps the cwd to a workitem (and quest). Campaign scope is always
// valid, so no scope resolving is not an error.
func inferScope(ctx context.Context, campRoot string, opts *addOptions) ledgerkit.Scope {
	scope := ledgerkit.Scope{
		Festival: opts.festival,
		Quest:    opts.quest,
	}
	if opts.workitem != "" {
		scope.Workitem = opts.workitem
		return scope
	}
	res, err := resolver.Resolve(ctx, campRoot, resolver.Options{})
	if err != nil || res == nil || res.Workitem == nil {
		return scope
	}
	scope.Workitem = res.Workitem.StableID
	if scope.Quest == "" {
		scope.Quest = res.QuestID
	}
	return scope
}
