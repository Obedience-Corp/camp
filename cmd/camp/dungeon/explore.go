package dungeon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

var exploreTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

var dungeonExploreCmd = &cobra.Command{
	Use:   "explore",
	Short: "Browse finished work across every dungeon",
	Long: `Browse finished work across every dungeon in this camp.

The feed is newest day first. Each row shows the work, the day it was put in
the dungeon, and which dungeon it came from. Festivals with a replay show that
replay on the focused row. Enter reads the item. g jumps the shell to it when
shell integration is installed.

[ and ] move between dungeons. s changes the status lens. The opening lens is
finished work: completed and done.

  camp dungeon explore
  camp dungeon explore --status archived
  camp dungeon explore --dungeon Festivals --query FA0024
  camp dungeon explore --json --since 2026-09-01

A pipe, or --json, prints the same index and does not open the feed.`,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Read-only dungeon feed; agents use --json and must not drive the TUI",
		"interactive":   "true",
	},
}

func init() {
	Cmd.AddCommand(dungeonExploreCmd)
	flags := dungeonExploreCmd.Flags()
	flags.Bool("json", false, "Print the feed as JSON")
	flags.String("status", "finished", "Status lens: finished, completed, done, archived, someday, killed, holding, all, or a status name")
	flags.String("dungeon", "all", "Dungeon lens: all, a label, or a camp-relative dungeon path")
	flags.String("since", "", "Include items put in the dungeon on or after this YYYY-MM-DD date")
	flags.String("until", "", "Include items put in the dungeon on or before this YYYY-MM-DD date")
	flags.String("query", "", "Case-insensitive substring matched against title, id, summary, dungeon, and path")
	flags.String("images", explore.ProtocolAuto, "Replay rendering: auto, kitty, iterm, or off")
	flags.Bool("no-media", false, "Do not draw replays")
	flags.String("path-output", "", "Write the selected item directory to a file (shell integration)")
	_ = flags.MarkHidden("path-output")

	// Set here, not in the literal: each closes over the command it is set on.
	jsonMode := func() bool { return exploreJSONMode(dungeonExploreCmd) }
	dungeonExploreCmd.Args = jsoncontract.Args(explore.SchemaVersion, jsonMode, exploreNoArgs)
	dungeonExploreCmd.RunE = jsoncontract.RunE(explore.SchemaVersion, jsonMode, func(cmd *cobra.Command, args []string) error {
		return exploreRefusal(cmd, runDungeonExplore(cmd, args))
	})
	dungeonExploreCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(explore.SchemaVersion, jsonMode))
}

// exploreJSONMode reports whether this run prints JSON. A pipe prints the
// index as JSON without --json, so a refusal on a pipe is the JSON envelope
// too: a caller parsing one must be able to parse the other.
func exploreJSONMode(cmd *cobra.Command) bool {
	asJSON, _ := cmd.Flags().GetBool("json")
	return asJSON || !exploreTerminal()
}

func exploreNoArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return exploreRefusal(cmd, camperrors.NewValidation("args", err.Error(), nil))
	}
	return nil
}

// exploreRefusal gives a refused flag or argument exit 2 (docs/exit-codes.md).
// The feed prints the reason here because main exits silently on a
// CommandError. In JSON mode the error envelope carries it instead.
func exploreRefusal(cmd *cobra.Command, err error) error {
	var refused *camperrors.ValidationError
	if !camperrors.As(err, &refused) || exploreJSONMode(cmd) {
		return err
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", refused.Message)
	return camperrors.NewCommand(cmd.CommandPath(), 2, refused.Message, err)
}

func runDungeonExplore(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	// Flag values are checked before the camp is read: a bad invocation is a
	// usage error wherever it is run.
	query, err := exploreQuery(cmd)
	if err != nil {
		return err
	}
	images, err := exploreImages(cmd)
	if err != nil {
		return err
	}
	cfg, root, err := config.ReadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}
	if exploreJSONMode(cmd) {
		idx, err := explore.Build(ctx, root)
		if err != nil {
			return err
		}
		result, err := explore.Apply(idx, query)
		if err != nil {
			return err
		}
		return writeExploreJSON(cmd, root, idx, result)
	}
	pathOutput, _ := cmd.Flags().GetString("path-output")
	cacheDir, err := explore.CacheDir(cfg.ID, root)
	if err != nil {
		cacheDir = ""
	}
	return runExploreTUI(cmd, root, cacheDir, query, images, pathOutput, plainExplore(cmd))
}

func exploreQuery(cmd *cobra.Command) (explore.Query, error) {
	status, _ := cmd.Flags().GetString("status")
	dungeon, _ := cmd.Flags().GetString("dungeon")
	since, _ := cmd.Flags().GetString("since")
	until, _ := cmd.Flags().GetString("until")
	text, _ := cmd.Flags().GetString("query")
	query := explore.Query{Status: status, Dungeon: dungeon, Since: since, Until: until, Text: text}
	if err := query.Validate(); err != nil {
		return explore.Query{}, err
	}
	return query, nil
}

func exploreImages(cmd *cobra.Command) (string, error) {
	noMedia, _ := cmd.Flags().GetBool("no-media")
	flag, _ := cmd.Flags().GetString("images")
	if noMedia {
		flag = explore.ProtocolOff
	}
	return explore.ChooseProtocol(flag, os.Getenv, plainExplore(cmd))
}

func plainExplore(cmd *cobra.Command) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	if cmd.Root() == nil {
		return false
	}
	v, err := cmd.Root().PersistentFlags().GetBool("no-color")
	return err == nil && v
}

func writeExploreJSON(cmd *cobra.Command, root string, idx explore.Index, result explore.Result) error {
	// Item paths are camp-relative. Consumers join them onto this root, which
	// docs/json-contracts.md requires to be symlink-resolved.
	campaignRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return camperrors.Wrap(err, "resolve camp root")
	}
	campaignRoot, err = filepath.Abs(campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolve camp root")
	}
	items := result.Items
	if items == nil {
		items = []explore.Item{}
	}
	doc := struct {
		SchemaVersion string         `json:"schema_version"`
		CampaignRoot  string         `json:"campaign_root"`
		DungeonLens   string         `json:"dungeon_lens"`
		StatusLens    string         `json:"status_lens"`
		Warnings      []string       `json:"warnings,omitempty"`
		Items         []explore.Item `json:"items"`
	}{
		SchemaVersion: explore.SchemaVersion,
		CampaignRoot:  campaignRoot,
		DungeonLens:   result.DungeonLens,
		StatusLens:    result.StatusLens,
		Warnings:      idx.Warnings,
		Items:         items,
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return camperrors.Wrap(err, "encoding dungeon feed")
	}
	return nil
}

func exploreJump(root string, item explore.Item) string {
	path := item.Path
	if !item.IsDir {
		path = parentSlash(path)
	}
	if root == "" {
		return path
	}
	return joinAbs(root, path)
}

func parentSlash(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			if i == 0 {
				return "/"
			}
			return path[:i]
		}
	}
	return "."
}

func joinAbs(root, rel string) string {
	if rel == "." {
		return root
	}
	return root + string(os.PathSeparator) + filepath.FromSlash(rel)
}
