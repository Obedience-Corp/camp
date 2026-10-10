package dungeon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

var exploreStdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

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
	Args: cobra.NoArgs,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Read-only dungeon feed; agents use --json and must not drive the TUI",
		"interactive":   "true",
	},
	RunE: runDungeonExplore,
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
}

func runDungeonExplore(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp directory")
	}
	query, err := exploreQuery(cmd)
	if err != nil {
		return err
	}
	images, err := exploreImages(cmd)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON || !exploreStdoutIsTTY() {
		idx, err := explore.Build(ctx, root)
		if err != nil {
			return err
		}
		result, err := explore.Apply(idx, query)
		if err != nil {
			return err
		}
		return writeExploreJSON(cmd, idx, result)
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
	if since != "" {
		if _, err := time.Parse("2006-01-02", since); err != nil {
			return explore.Query{}, camperrors.Newf("--since must be YYYY-MM-DD, got %q", since)
		}
	}
	if until != "" {
		if _, err := time.Parse("2006-01-02", until); err != nil {
			return explore.Query{}, camperrors.Newf("--until must be YYYY-MM-DD, got %q", until)
		}
	}
	if since != "" && until != "" && since > until {
		return explore.Query{}, camperrors.New("--since is after --until")
	}
	return explore.Query{Status: status, Dungeon: dungeon, Since: since, Until: until, Text: text}, nil
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

func writeExploreJSON(cmd *cobra.Command, idx explore.Index, result explore.Result) error {
	items := result.Items
	if items == nil {
		items = []explore.Item{}
	}
	doc := struct {
		SchemaVersion string         `json:"schema_version"`
		DungeonLens   string         `json:"dungeon_lens"`
		StatusLens    string         `json:"status_lens"`
		Warnings      []string       `json:"warnings,omitempty"`
		Items         []explore.Item `json:"items"`
	}{
		SchemaVersion: explore.SchemaVersion,
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
