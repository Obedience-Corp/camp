package festivals

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/spf13/cobra"
)

const schemaVersion = "camp-festivals/v1"

type festTasks struct {
	Completed int `json:"completed"`
	Total     int `json:"total"`
}

type festEntry struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Path      string    `json:"path"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
	Tasks     festTasks `json:"tasks"`
}

type progress struct {
	Completed int `json:"completed"`
	Total     int `json:"total"`
}

type festivalItem struct {
	Campaign  string   `json:"campaign"`
	Org       string   `json:"org"`
	Festival  string   `json:"festival"`
	Status    string   `json:"status"`
	Progress  progress `json:"progress"`
	Path      string   `json:"path,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

type festivalsFilter struct {
	Org  string   `json:"org"`
	Tags []string `json:"tags"`
}

type festivalsOutput struct {
	SchemaVersion string          `json:"schema_version"`
	Filter        festivalsFilter `json:"filter"`
	Items         []festivalItem  `json:"items"`
}

var Cmd = &cobra.Command{
	Use:     "festivals",
	Short:   "List festivals across campaigns, filtered by org/tag",
	GroupID: "registry",
	Long: `Aggregate festivals across campaigns, filtered by campaign org/tag.

Selects campaigns from the registry by --org and --tag (AND), then composes
'fest list --json' in each matching campaign and aggregates the result. The
campaign set defaults to active campaigns; --all-campaigns includes inactive and
reference campaigns. Festival-level flags (--status, --all, --since, --until,
--sort) are passed through to each underlying 'fest list'.

Runs one 'fest list' per matching campaign (sequentially); campaigns without a
festivals/ workspace contribute nothing. Read-only.`,
	Example: `  camp festivals --org obey
  camp festivals --org obey --status active
  camp festivals --tag paid-work --all-campaigns --json`,
	Args: cobra.NoArgs,
	RunE: runFestivals,
}

func init() {
	Cmd.Flags().String("org", "", "Only campaigns in this org")
	Cmd.Flags().StringSlice("tag", nil, "Only campaigns carrying this tag (repeat for AND)")
	Cmd.Flags().Bool("all-campaigns", false, "Include inactive/reference campaigns (default: active only)")
	Cmd.Flags().Bool("json", false, "Output as JSON")
	Cmd.Flags().String("status", "", "Festival status filter, passed to fest list")
	Cmd.Flags().Bool("all", false, "Include completed/dungeon festivals, passed to fest list")
	Cmd.Flags().String("since", "", "Festivals created on or after this date, passed to fest list")
	Cmd.Flags().String("until", "", "Festivals created on or before this date, passed to fest list")
	Cmd.Flags().String("sort", "", "Festival sort, passed to fest list")
}

func runFestivals(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	org, _ := cmd.Flags().GetString("org")
	tags, _ := cmd.Flags().GetStringSlice("tag")
	allCampaigns, _ := cmd.Flags().GetBool("all-campaigns")
	asJSON, _ := cmd.Flags().GetBool("json")

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	campaigns := selectCampaigns(reg, org, tags, allCampaigns)

	festPath, err := fest.FindFestCLI()
	if err != nil {
		return camperrors.Wrap(err, "camp festivals needs the fest CLI")
	}

	passthrough := passthroughFlags(cmd)
	items, err := aggregate(ctx, festPath, campaigns, passthrough)
	if err != nil {
		return err
	}

	if asJSON {
		out := festivalsOutput{SchemaVersion: schemaVersion, Filter: festivalsFilter{Org: org, Tags: tagsOrEmpty(tags)}, Items: items}
		return encodeJSON(cmd.OutOrStdout(), out)
	}
	return renderFestivalsHuman(cmd.OutOrStdout(), items, reg.FallbackOrg())
}

func selectCampaigns(reg *config.Registry, org string, tags []string, allCampaigns bool) []config.RegisteredCampaign {
	var out []config.RegisteredCampaign
	for _, c := range reg.ListAll() {
		if !allCampaigns && c.Status != config.StatusActive {
			continue
		}
		if org != "" && c.Org != org {
			continue
		}
		if !campaignHasAllTags(c, tags) {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Org != out[j].Org {
			return out[i].Org < out[j].Org
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func campaignHasAllTags(c config.RegisteredCampaign, tags []string) bool {
	for _, want := range tags {
		found := false
		for _, t := range c.Tags {
			if t == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func aggregate(ctx context.Context, festPath string, campaigns []config.RegisteredCampaign, passthrough []string) ([]festivalItem, error) {
	items := []festivalItem{}
	for _, c := range campaigns {
		if !hasFestivalsWorkspace(c.Path) {
			continue
		}
		entries, err := runFestList(ctx, festPath, c.Path, passthrough)
		if err != nil {
			return nil, camperrors.Wrapf(err, "fest list failed for campaign %q (%s)", c.Name, c.Path)
		}
		for _, e := range entries {
			items = append(items, festivalItem{
				Campaign:  c.Name,
				Org:       c.Org,
				Festival:  e.Name,
				Status:    e.Status,
				Progress:  progress{Completed: e.Tasks.Completed, Total: e.Tasks.Total},
				Path:      e.Path,
				CreatedAt: e.CreatedAt,
				UpdatedAt: e.UpdatedAt,
			})
		}
	}
	return items, nil
}

func hasFestivalsWorkspace(campaignPath string) bool {
	info, err := os.Stat(filepath.Join(campaignPath, "festivals"))
	return err == nil && info.IsDir()
}

func runFestList(ctx context.Context, festPath, campaignPath string, passthrough []string) ([]festEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	args := append([]string{"list", "--json", "--progress"}, passthrough...)
	c := exec.CommandContext(ctx, festPath, args...)
	c.Dir = campaignPath
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, camperrors.Wrapf(err, "fest list --json failed: %s", detail)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, camperrors.Wrap(err, "parsing fest list --json output")
	}
	var entries []festEntry
	for _, rawVal := range raw {
		trimmed := bytes.TrimSpace(rawVal)
		if len(trimmed) == 0 || trimmed[0] != '[' {
			continue
		}
		var group []festEntry
		if err := json.Unmarshal(rawVal, &group); err != nil {
			return nil, camperrors.Wrap(err, "parsing fest list --json status group")
		}
		entries = append(entries, group...)
	}
	return entries, nil
}

func passthroughFlags(cmd *cobra.Command) []string {
	var args []string
	for _, name := range []string{"status", "since", "until", "sort"} {
		if v, _ := cmd.Flags().GetString(name); v != "" {
			args = append(args, "--"+name, v)
		}
	}
	if v, _ := cmd.Flags().GetBool("all"); v {
		args = append(args, "--all")
	}
	return args
}

func tagsOrEmpty(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}
