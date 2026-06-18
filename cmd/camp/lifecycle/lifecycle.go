package lifecycle

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

type statusSetResult struct {
	Campaign string `json:"campaign"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type statusCount struct {
	Status    string `json:"status"`
	Campaigns int    `json:"campaigns"`
}

var lifecycleSetCmd = &cobra.Command{
	Use:   "set <campaign> <status>",
	Short: "Set a campaign's lifecycle status",
	Long: `Transition a campaign to one of: active, inactive, reference.

Any other value is rejected. Setting inactive or reference does not unregister
the campaign.`,
	Example: `  camp lifecycle set old-project reference`,
	Args:    cobra.ExactArgs(2),
	RunE:    runLifecycleSet,
}

var lifecycleListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List status counts across the registry",
	Example: `  camp lifecycle list`,
	Args:    cobra.NoArgs,
	RunE:    runLifecycleList,
}

func init() {
	Cmd.AddCommand(lifecycleSetCmd)
	Cmd.AddCommand(lifecycleListCmd)
	lifecycleSetCmd.Flags().Bool("json", false, "Output as JSON")
	lifecycleListCmd.Flags().Bool("json", false, "Output as JSON")
}

func runLifecycleSet(cmd *cobra.Command, args []string) error {
	campaignQuery, status := args[0], args[1]
	if err := config.ValidateStatus(status); err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	var result statusSetResult
	err := config.UpdateRegistry(cmd.Context(), func(reg *config.Registry) error {
		c, ok := reg.Get(campaignQuery)
		if !ok {
			return camperrors.NewNotFound("campaign", campaignQuery, nil)
		}
		result = statusSetResult{Campaign: c.Name, From: c.Status, To: status}
		entry := reg.Campaigns[c.ID]
		entry.Status = status
		reg.Campaigns[c.ID] = entry
		return nil
	})
	if err != nil {
		return err
	}
	if asJSON {
		return encodeJSON(cmd.OutOrStdout(), result)
	}
	if result.From == result.To {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%q status already %s\n", result.Campaign, result.To)
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "set %q status: %s -> %s\n", result.Campaign, result.From, result.To)
	return err
}

func runLifecycleList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	reg, err := config.LoadRegistry(cmd.Context())
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	counts := computeStatusCounts(reg)
	if asJSON {
		return encodeJSON(cmd.OutOrStdout(), counts)
	}
	return writeStatusCounts(cmd.OutOrStdout(), counts)
}

func computeStatusCounts(reg *config.Registry) []statusCount {
	byStatus := make(map[string]int)
	for _, c := range reg.ListAll() {
		byStatus[c.Status]++
	}
	out := make([]statusCount, 0, len(config.ValidStatuses()))
	for _, s := range config.ValidStatuses() {
		out = append(out, statusCount{Status: s, Campaigns: byStatus[s]})
	}
	return out
}

func writeStatusCounts(w io.Writer, counts []statusCount) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "STATUS\tCAMPAIGNS"); err != nil {
		return err
	}
	for _, c := range counts {
		if _, err := fmt.Fprintf(tw, "%s\t%d\n", c.Status, c.Campaigns); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
