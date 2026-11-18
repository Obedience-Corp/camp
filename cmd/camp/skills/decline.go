package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	intskills "github.com/Obedience-Corp/camp/internal/skills"
	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/spf13/cobra"
)

// SkillsDeclineJSONVersion is the schema of `camp skills decline --json`.
const SkillsDeclineJSONVersion = "skills-decline/v1alpha1"

var skillsDeclineJSON bool

var skillsDeclineCmd = &cobra.Command{
	Use:   "decline <slug>",
	Short: "Remove a plugin skill bundle and keep it out of this camp",
	Long: `Remove a skill bundle a plugin offered, such as festival-app from camp-app,
together with the links camp projected for it, and record in
.campaign/skills/.shipped.yaml that this camp declined it. The record travels
with the camp, so an automatic request ('camp skills update --auto') never adds
the bundle again, on any machine. Naming it in 'camp skills update <slug>'
adds it back.

A bundle with files camp did not write is refused unless --force, which first
secures an undo: 'git checkout' when the bundle is committed and clean,
otherwise a copy under .campaign/skills/.backup/. A bundle that is, or holds, a
symlink is always refused. Bundles camp ships cannot be declined.`,
	Example: `  camp skills decline festival-app          Remove the Festival app skill from this camp
  camp skills decline festival-app --force  Remove it even though it was edited
  camp skills decline festival-app --json   Machine-readable result`,
	Args: jsoncontract.Args(SkillsDeclineJSONVersion, func() bool { return skillsDeclineJSON }, cobra.ExactArgs(1)),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive; refuses edited bundles unless --force, which prints an undo",
	},
	RunE: jsoncontract.RunE(SkillsDeclineJSONVersion, func() bool { return skillsDeclineJSON }, runSkillsDecline),
}

func init() {
	Cmd.AddCommand(skillsDeclineCmd)
	flags := skillsDeclineCmd.Flags()
	flags.BoolP("force", "f", false, "Also remove a bundle with local changes, after securing an undo")
	flags.BoolVar(&skillsDeclineJSON, "json", false, "Print one result as JSON")
	skillsDeclineCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(SkillsDeclineJSONVersion, func() bool { return skillsDeclineJSON }))
}

type skillsDeclineResult struct {
	SchemaVersion string   `json:"schema_version"`
	Slug          string   `json:"slug"`
	Source        string   `json:"source"`
	Removed       bool     `json:"removed"`
	LinksRemoved  []string `json:"links_removed"`
	Undo          string   `json:"undo"`
	Restore       string   `json:"restore,omitempty"`
}

func runSkillsDecline(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	slug := args[0]
	force, _ := cmd.Flags().GetBool("force")
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}
	pluginName, err := declinedPlugin(cmd, root, slug)
	if err != nil {
		return err
	}
	res, err := intskills.Decline(ctx, intskills.DeclineOptions{
		Root:          root,
		Slug:          slug,
		Plugin:        pluginName,
		Force:         force,
		WorktreesRoot: worktreesRoot(cmd, root),
		CampVersion:   version.Version,
	})
	if err != nil {
		return declineFailure(slug, err, res)
	}
	result := skillsDeclineResult{
		SchemaVersion: SkillsDeclineJSONVersion,
		Slug:          slug,
		Source:        "plugin:" + pluginName,
		Removed:       res.Removed,
		LinksRemoved:  append([]string{}, res.LinksRemoved...),
		Undo:          "camp skills update " + slug,
	}
	if res.Restore != nil {
		result.Restore = res.Restore.Command
	}
	if skillsDeclineJSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return camperrors.Wrap(err, "marshal JSON")
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}
	return printDecline(cmd.OutOrStdout(), result, res)
}

// declinedPlugin names the plugin behind slug: the manifest's record, or else
// camp-app when it offers the bundle. A bundle camp ships, or one no plugin
// offers, is refused.
func declinedPlugin(cmd *cobra.Command, root, slug string) (string, error) {
	ctx := cmd.Context()
	shipped, err := scaffold.ShippedSkills(ctx, root)
	if err != nil {
		return "", err
	}
	campSlugs := intskills.ShippedSlugs(shipped)
	if !slices.Contains(campSlugs, slug) && !intskills.ValidPluginBundleName(slug) {
		return "", usageError(cmd, skillsDeclineJSON, "slug", fmt.Sprintf("%q is not a plugin skill bundle name", slug))
	}
	if slices.Contains(campSlugs, slug) {
		return "", usageError(cmd, skillsDeclineJSON, "slug", slug+" is a bundle camp ships; decline is only for plugin skill bundles")
	}
	manifest, err := intskills.ReadManifest(os.DirFS(filepath.Join(root, campaign.CampaignDir, intskills.SkillsSubdir)))
	if err != nil {
		return "", err
	}
	if manifest != nil {
		if rec, ok := manifest.Providers[slug]; ok && rec.Plugin != "" {
			return rec.Plugin, nil
		}
	}
	report, err := intskills.AskProvider(ctx, intskills.CampAppPlugin, campAppRunner, campSlugs)
	if err != nil {
		return "", err
	}
	if _, ok := report.Offered(slug); ok {
		return report.Plugin, nil
	}
	reason := report.Plugin + " does not offer it"
	if report.Status != intskills.ProviderOK {
		reason = report.Detail
	}
	return "", usageError(cmd, skillsDeclineJSON, "slug",
		fmt.Sprintf("%s is not a plugin skill bundle of this camp: the camp has no record of it and %s", slug, reason))
}

// declineFailure explains a refused or failed decline. When the decline had
// already backed up the files it was removing, the way to restore them is part
// of the error, in the message and as the JSON hint, so it is never lost.
func declineFailure(slug string, err error, res *intskills.DeclineResult) error {
	switch {
	case errors.Is(err, intskills.ErrDeclineEdited):
		return camperrors.Wrapf(err, "decline %s: run 'camp skills decline --force %s' to remove it after securing an undo", slug, slug)
	case errors.Is(err, intskills.ErrDeclineShape):
		return camperrors.Newf("decline %s: %v; camp left it as it is; remove it yourself if you mean to", slug, err)
	case res != nil && res.Restore != nil:
		restore := "restore the removed files: " + res.Restore.Command
		return jsoncontract.WithHint(camperrors.Wrapf(err, "decline %s stopped; %s", slug, restore), restore)
	}
	return err
}

func worktreesRoot(cmd *cobra.Command, root string) string {
	cfg, err := config.LoadCampaignConfig(cmd.Context(), root)
	if err != nil {
		cfg = &config.CampaignConfig{}
	}
	return paths.NewResolver(root, cfg.Paths()).Worktrees()
}

func printDecline(out io.Writer, result skillsDeclineResult, res *intskills.DeclineResult) error {
	var lines []string
	if result.Removed {
		lines = append(lines, "removed .campaign/skills/"+result.Slug)
	}
	for _, link := range result.LinksRemoved {
		lines = append(lines, "removed link "+link)
	}
	if res.ManifestWritten {
		lines = append(lines, fmt.Sprintf("declined %s in .campaign/skills/%s; automatic requests leave it out of this camp", result.Slug, intskills.ManifestFileName))
	} else {
		lines = append(lines, fmt.Sprintf("%s was already declined in this camp", result.Slug))
	}
	if res.Restore != nil {
		lines = append(lines, "restore the removed files: "+res.Restore.Command)
	}
	lines = append(lines, "to add it back: "+result.Undo)
	return writeLines(out, lines)
}
