//go:build dev

package quest

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/quest"
)

const QuestLinksJSONVersion = "quest-links/v1alpha1"

var questLinksJSON bool

var questLinksCmd = &cobra.Command{
	Use:   "links <quest>",
	Short: "List linked artifacts for a quest",
	Long: `Display all campaign artifacts linked to a quest.

Default output is a table. Use --json for machine-readable output.

Examples:
  camp quest links myquest
  camp quest links myquest --json`,
	Args: jsoncontract.Args(QuestLinksJSONVersion, func() bool { return questLinksJSON }, cobra.ExactArgs(1)),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest links listing",
	},
	RunE: jsoncontract.RunE(QuestLinksJSONVersion, func() bool { return questLinksJSON }, runQuestLinks),
}

func init() {
	Cmd.AddCommand(questLinksCmd)

	questLinksCmd.Flags().BoolVar(&questLinksJSON, "json", false, "Output as JSON")
	questLinksCmd.ValidArgsFunction = completeQuestSelector
	questLinksCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestLinksJSONVersion, func() bool { return questLinksJSON }))
}

func runQuestLinks(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	selector := args[0]

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	links, err := qctx.service.Links(ctx, selector)
	if err != nil {
		return err
	}

	if questLinksJSON {
		return outputLinksJSON(qctx, links)
	}
	return outputLinksTable(links)
}

type questLinksJSONPayload struct {
	SchemaVersion string       `json:"schema_version"`
	CampaignRoot  string       `json:"campaign_root"`
	Links         []quest.Link `json:"links"`
}

func outputLinksJSON(qctx *questCommandContext, links []quest.Link) error {
	if links == nil {
		links = []quest.Link{}
	}
	relLinks := make([]quest.Link, len(links))
	for i, link := range links {
		relPath, err := pathutil.RelativeToRoot(qctx.campaignRoot, link.Path)
		if err != nil {
			return camperrors.Wrap(err, "relativizing link path")
		}
		link.Path = relPath
		relLinks[i] = link
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(questLinksJSONPayload{
		SchemaVersion: QuestLinksJSONVersion,
		CampaignRoot:  qctx.campaignRoot,
		Links:         relLinks,
	})
}

func outputLinksTable(links []quest.Link) error {
	if len(links) == 0 {
		fmt.Println("No linked artifacts.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PATH\tTYPE\tADDED")
	for _, link := range links {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n",
			link.Path,
			link.Type,
			link.AddedAt.Format("2006-01-02 15:04"),
		)
	}
	_ = w.Flush()
	fmt.Printf("\n%d link(s)\n", len(links))
	return nil
}
