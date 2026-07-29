package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Selector and scope parsing for `camp switch`. Split out of switch.go to keep
// that file under the 500-line ceiling; behavior is unchanged.

func switchScopeFromFlags(cmd *cobra.Command) (cmdutil.CampaignScope, error) {
	org, _ := cmd.Flags().GetString("org")
	status, _ := cmd.Flags().GetString("status")
	all, _ := cmd.Flags().GetBool("all")
	if org != "" {
		if err := config.ValidateName("org", org); err != nil {
			return cmdutil.CampaignScope{}, err
		}
	}
	if status != "" {
		if err := config.ValidateStatus(status); err != nil {
			return cmdutil.CampaignScope{}, err
		}
	}
	if status != "" && all {
		return cmdutil.CampaignScope{}, camperrors.New("cannot use --status with --all")
	}
	return cmdutil.CampaignScope{Org: org, Status: status, All: all}, nil
}

func parseSwitchArg(raw string, scope cmdutil.CampaignScope) (cmdutil.ParsedSwitchSelector, error) {
	parsed := cmdutil.ParseSwitchSelector(raw)
	if parsed.Org != "" {
		if err := config.ValidateName("org", parsed.Org); err != nil {
			return parsed, err
		}
		if strings.Contains(parsed.Campaign, "/") {
			return parsed, camperrors.New("switch selector may contain at most one org separator")
		}
		if parsed.Campaign == "" {
			return parsed, camperrors.New("campaign name required after org selector")
		}
		if scope.Org != "" && scope.Org != parsed.Org {
			return parsed, camperrors.New(fmt.Sprintf("selector org %q conflicts with --org %q", parsed.Org, scope.Org))
		}
	}
	return parsed, nil
}
