package tag

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

type tagChangeResult struct {
	Campaign string   `json:"campaign"`
	Added    []string `json:"added,omitempty"`
	Removed  []string `json:"removed,omitempty"`
	Tags     []string `json:"tags"`
}

type tagCount struct {
	Tag       string `json:"tag"`
	Campaigns int    `json:"campaigns"`
}

var tagAddCmd = &cobra.Command{
	Use:   "add <campaign> <tag>...",
	Short: "Add tags to a campaign",
	Long: `Add one or more tags to a campaign (set semantics).

Re-adding a tag the campaign already carries is a no-op for that tag. Each tag
name must be lowercase letters, digits, and hyphens with no leading digit.`,
	Example: `  camp tag add obey-campaign paid-work q3-2026`,
	Args:    cobra.MinimumNArgs(2),
	RunE:    runTagAdd,
}

var tagRmCmd = &cobra.Command{
	Use:     "rm <campaign> <tag>...",
	Aliases: []string{"remove"},
	Short:   "Remove tags from a campaign",
	Long: `Remove one or more tags from a campaign.

Removing a tag the campaign does not carry is a no-op for that tag.`,
	Example: `  camp tag rm obey-campaign q3-2026`,
	Args:    cobra.MinimumNArgs(2),
	RunE:    runTagRm,
}

var tagListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all tags in use with campaign counts",
	Example: `  camp tag list`,
	Args:    cobra.NoArgs,
	RunE:    runTagList,
}

func init() {
	Cmd.AddCommand(tagAddCmd)
	Cmd.AddCommand(tagRmCmd)
	Cmd.AddCommand(tagListCmd)
	tagAddCmd.Flags().Bool("json", false, "Output as JSON")
	tagRmCmd.Flags().Bool("json", false, "Output as JSON")
	tagListCmd.Flags().Bool("json", false, "Output as JSON")
}

func runTagAdd(cmd *cobra.Command, args []string) error {
	campaignQuery, tags := args[0], args[1:]
	for _, tg := range tags {
		if err := config.ValidateName("tag", tg); err != nil {
			return err
		}
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	result := tagChangeResult{Added: []string{}}
	err := config.UpdateRegistry(cmd.Context(), func(reg *config.Registry) error {
		c, ok := reg.Get(campaignQuery)
		if !ok {
			return camperrors.NewNotFound("campaign", campaignQuery, nil)
		}
		next, added := mergeTags(c.Tags, tags)
		entry := reg.Campaigns[c.ID]
		entry.Tags = next
		reg.Campaigns[c.ID] = entry
		result.Campaign = c.Name
		result.Added = added
		result.Tags = next
		return nil
	})
	if err != nil {
		return err
	}
	return renderTagChange(cmd.OutOrStdout(), result, asJSON, true)
}

func runTagRm(cmd *cobra.Command, args []string) error {
	campaignQuery, tags := args[0], args[1:]
	for _, tg := range tags {
		if err := config.ValidateName("tag", tg); err != nil {
			return err
		}
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	result := tagChangeResult{Removed: []string{}}
	err := config.UpdateRegistry(cmd.Context(), func(reg *config.Registry) error {
		c, ok := reg.Get(campaignQuery)
		if !ok {
			return camperrors.NewNotFound("campaign", campaignQuery, nil)
		}
		next, removed := removeTags(c.Tags, tags)
		entry := reg.Campaigns[c.ID]
		entry.Tags = next
		reg.Campaigns[c.ID] = entry
		result.Campaign = c.Name
		result.Removed = removed
		result.Tags = next
		return nil
	})
	if err != nil {
		return err
	}
	return renderTagChange(cmd.OutOrStdout(), result, asJSON, false)
}

func runTagList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	reg, err := config.LoadRegistry(cmd.Context())
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	counts := computeTagCounts(reg)
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(counts)
	}
	return writeTagCounts(cmd.OutOrStdout(), counts)
}

func mergeTags(existing, add []string) (result, added []string) {
	set := make(map[string]bool, len(existing)+len(add))
	for _, t := range existing {
		set[t] = true
	}
	added = []string{}
	for _, t := range add {
		if !set[t] {
			set[t] = true
			added = append(added, t)
		}
	}
	return sortedKeys(set), added
}

func removeTags(existing, rm []string) (result, removed []string) {
	drop := make(map[string]bool, len(rm))
	for _, t := range rm {
		drop[t] = true
	}
	keep := make(map[string]bool, len(existing))
	removed = []string{}
	for _, t := range existing {
		if drop[t] {
			removed = append(removed, t)
			continue
		}
		keep[t] = true
	}
	return sortedKeys(keep), removed
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func computeTagCounts(reg *config.Registry) []tagCount {
	byTag := make(map[string]int)
	for _, c := range reg.ListAll() {
		for _, t := range c.Tags {
			byTag[t]++
		}
	}
	out := make([]tagCount, 0, len(byTag))
	for t, n := range byTag {
		out = append(out, tagCount{Tag: t, Campaigns: n})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Tag < out[j].Tag
	})
	return out
}

func renderTagChange(w io.Writer, r tagChangeResult, asJSON, isAdd bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	_, err := io.WriteString(w, formatTagChange(r, isAdd))
	return err
}

func formatTagChange(r tagChangeResult, isAdd bool) string {
	now := "none"
	if len(r.Tags) > 0 {
		now = strings.Join(r.Tags, ", ")
	}
	changed := r.Added
	verb, sign := "tagged", "+"
	if !isAdd {
		changed = r.Removed
		verb, sign = "untagged", "-"
	}
	if len(changed) == 0 {
		return fmt.Sprintf("%q unchanged (now: %s)\n", r.Campaign, now)
	}
	parts := make([]string, len(changed))
	for i, t := range changed {
		parts[i] = sign + t
	}
	return fmt.Sprintf("%s %q: %s (now: %s)\n", verb, r.Campaign, strings.Join(parts, " "), now)
}

func writeTagCounts(w io.Writer, counts []tagCount) error {
	if len(counts) == 0 {
		_, err := io.WriteString(w, "no tags in use\n")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "TAG\tCAMPAIGNS"); err != nil {
		return err
	}
	for _, c := range counts {
		if _, err := fmt.Fprintf(tw, "%s\t%d\n", c.Tag, c.Campaigns); err != nil {
			return err
		}
	}
	return tw.Flush()
}
