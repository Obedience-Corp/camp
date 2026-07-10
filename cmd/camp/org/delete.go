package org

import (
	"encoding/json"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

type orgDeleteResult struct {
	Org        string   `json:"org"`
	Deleted    bool     `json:"deleted"`
	Force      bool     `json:"force"`
	Reassigned []string `json:"reassigned,omitempty"`
}

var orgDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an org (empty only unless --force)",
	Long: `Delete a first-class org from the registry.

Empty orgs delete without flags. Orgs with members require --force, which
reassigns every member to the fallback org and then deletes the org.

The fallback org cannot be deleted.`,
	Example: `  camp org delete empty-org
  camp org delete obey --force
  camp org delete empty-org --json`,
	Args: cobra.ExactArgs(1),
	RunE: runOrgDelete,
}

func init() {
	Cmd.AddCommand(orgDeleteCmd)
	orgDeleteCmd.Flags().Bool("force", false, "Reassign members to the fallback org, then delete")
	orgDeleteCmd.Flags().Bool("json", false, "Output as JSON")
}

func runOrgDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := validateOrgName(name); err != nil {
		return err
	}
	force, _ := cmd.Flags().GetBool("force")
	asJSON, _ := cmd.Flags().GetBool("json")

	result := orgDeleteResult{Org: name, Force: force, Reassigned: []string{}}
	err := config.UpdateRegistry(cmd.Context(), func(reg *config.Registry) error {
		if name == reg.FallbackOrg() {
			return camperrors.NewValidation("org", "cannot delete the fallback org", nil)
		}
		if !orgExists(reg, name) {
			return camperrors.NewNotFound("org", name, nil)
		}
		members := membersOf(reg, name)
		if len(members) > 0 {
			if !force {
				return camperrors.NewValidation("org",
					fmt.Sprintf("org %q has %d member(s); pass --force to reassign them to %q and delete",
						name, len(members), reg.FallbackOrg()), nil)
			}
			fallback := reg.FallbackOrg()
			for _, c := range members {
				entry := reg.Campaigns[c.ID]
				entry.Org = fallback
				reg.Campaigns[c.ID] = entry
				result.Reassigned = append(result.Reassigned, c.Name)
			}
		}
		removeOrg(reg, name)
		result.Deleted = true
		return nil
	})
	if err != nil {
		return err
	}
	return renderOrgDeleteResult(cmd, result, asJSON)
}

func renderOrgDeleteResult(cmd *cobra.Command, r orgDeleteResult, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	if len(r.Reassigned) > 0 {
		_, err := fmt.Fprintf(cmd.OutOrStdout(),
			"deleted org %q (reassigned %d member(s) to fallback)\n", r.Org, len(r.Reassigned))
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "deleted org %q\n", r.Org)
	return err
}
