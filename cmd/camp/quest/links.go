//go:build dev

package quest

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/quest"
)

var questLinksCmd = &cobra.Command{
	Use:   "links <quest>",
	Short: "List linked artifacts for a quest",
	Long: `Display all campaign artifacts linked to a quest.

Default output is a table. Use --json for machine-readable output.

Examples:
  camp quest links myquest
  camp quest links myquest --json`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest links listing",
	},
	RunE: runQuestLinks,
}

func init() {
	Cmd.AddCommand(questLinksCmd)

	questLinksCmd.Flags().Bool("json", false, "Output as JSON")
	questLinksCmd.ValidArgsFunction = completeQuestSelector
}

func runQuestLinks(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	selector := args[0]
	asJSON, _ := cmd.Flags().GetBool("json")

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	links, err := qctx.service.Links(ctx, selector)
	if err != nil {
		return err
	}

	if asJSON {
		return outputLinksJSON(links)
	}
	return outputLinksTable(links)
}

func outputLinksJSON(links []quest.Link) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if links == nil {
		links = []quest.Link{}
	}
	return enc.Encode(links)
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
