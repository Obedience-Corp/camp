package cmdutil

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav/fuzzy"
)

// CampaignScope describes the candidate set for switch resolution.
type CampaignScope struct {
	Org    string
	Status string
	All    bool
}

// ParsedSwitchSelector is the parsed campaign[@tab] or org/campaign[@tab]
// selector accepted by camp switch.
type ParsedSwitchSelector struct {
	Org      string
	Campaign string
	Tab      string
	HasTab   bool
}

// ParseSwitchSelector splits the switch selector into org, campaign, and tab
// parts. Validation that depends on completion/direct mode stays with callers.
func ParseSwitchSelector(raw string) ParsedSwitchSelector {
	left := raw
	parsed := ParsedSwitchSelector{}
	if at := strings.IndexByte(raw, '@'); at >= 0 {
		left = raw[:at]
		parsed.Tab = raw[at+1:]
		parsed.HasTab = true
	}
	if slash := strings.IndexByte(left, '/'); slash >= 0 {
		parsed.Org = left[:slash]
		parsed.Campaign = left[slash+1:]
		return parsed
	}
	parsed.Campaign = left
	return parsed
}

// FilterCampaigns returns the registry entries visible under scope. Direct
// switch, picker, and completion all default to active campaigns unless --all or
// --status changes the lifecycle policy.
func FilterCampaigns(reg *config.Registry, scope CampaignScope) []config.RegisteredCampaign {
	all := reg.ListAll()
	out := make([]config.RegisteredCampaign, 0, len(all))
	fallbackOrg := reg.FallbackOrg()
	for _, c := range all {
		if c.Org == "" {
			c.Org = fallbackOrg
		}
		if c.Status == "" {
			c.Status = config.StatusActive
		}
		if scope.Org != "" && c.Org != scope.Org {
			continue
		}
		if scope.Status != "" {
			if c.Status != scope.Status {
				continue
			}
		} else if !scope.All && c.Status != config.StatusActive {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ResolveCampaignSelectionScoped applies the existing switch lookup order
// inside the filtered candidate set: exact ID, unique ID prefix, exact name, and
// fuzzy name.
func ResolveCampaignSelectionScoped(query string, reg *config.Registry, scope CampaignScope, matchWriter io.Writer) (config.RegisteredCampaign, error) {
	candidates := FilterCampaigns(reg, scope)
	return resolveCampaignFromCandidates(query, candidates, scope, matchWriter)
}

func resolveCampaignFromCandidates(query string, candidates []config.RegisteredCampaign, scope CampaignScope, matchWriter io.Writer) (config.RegisteredCampaign, error) {
	if query == "" {
		return config.RegisteredCampaign{}, camperrors.New("campaign name required" + scopeDescription(scope))
	}

	for _, c := range candidates {
		if c.ID == query {
			return c, nil
		}
	}

	var idPrefixMatches []config.RegisteredCampaign
	for _, c := range candidates {
		if strings.HasPrefix(c.ID, query) {
			idPrefixMatches = append(idPrefixMatches, c)
		}
	}
	switch len(idPrefixMatches) {
	case 1:
		return idPrefixMatches[0], nil
	case 0:
	default:
		return config.RegisteredCampaign{}, camperrors.New(fmt.Sprintf("campaign ID prefix %q is ambiguous%s: %s",
			query, scopeDescription(scope), campaignIDs(idPrefixMatches)))
	}

	var exactNameMatches []config.RegisteredCampaign
	for _, c := range candidates {
		if c.Name == query {
			exactNameMatches = append(exactNameMatches, c)
		}
	}
	switch len(exactNameMatches) {
	case 1:
		return exactNameMatches[0], nil
	case 0:
	default:
		return config.RegisteredCampaign{}, camperrors.New(fmt.Sprintf("campaign name %q is ambiguous%s: %s",
			query, scopeDescription(scope), campaignIDs(exactNameMatches)))
	}

	names := campaignNames(candidates)
	matches := fuzzy.Filter(names, query)
	if len(matches) == 0 {
		return config.RegisteredCampaign{}, camperrors.New(fmt.Sprintf("campaign %q not found%s", query, scopeDescription(scope)))
	}

	bestName := matches[0].Target
	var fuzzyMatches []config.RegisteredCampaign
	for _, c := range candidates {
		if c.Name == bestName {
			fuzzyMatches = append(fuzzyMatches, c)
		}
	}
	if len(fuzzyMatches) != 1 {
		return config.RegisteredCampaign{}, camperrors.New(fmt.Sprintf("campaign name %q is ambiguous%s: %s",
			bestName, scopeDescription(scope), campaignIDs(fuzzyMatches)))
	}

	if matchWriter != nil {
		_, _ = fmt.Fprintf(matchWriter, "Matched: %s -> %s\n", query, fuzzyMatches[0].Name)
	}
	return fuzzyMatches[0], nil
}

func campaignNames(campaigns []config.RegisteredCampaign) []string {
	names := make([]string, 0, len(campaigns))
	seen := make(map[string]struct{}, len(campaigns))
	for _, c := range campaigns {
		if _, ok := seen[c.Name]; ok {
			continue
		}
		seen[c.Name] = struct{}{}
		names = append(names, c.Name)
	}
	return names
}

func campaignIDs(campaigns []config.RegisteredCampaign) string {
	ids := make([]string, 0, len(campaigns))
	for _, c := range campaigns {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}

func scopeDescription(scope CampaignScope) string {
	var parts []string
	if scope.Org != "" {
		parts = append(parts, fmt.Sprintf("org %q", scope.Org))
	}
	if scope.Status != "" {
		parts = append(parts, fmt.Sprintf("status %q", scope.Status))
	} else if !scope.All {
		parts = append(parts, fmt.Sprintf("status %q", config.StatusActive))
	}
	if len(parts) == 0 {
		return ""
	}
	return " in " + strings.Join(parts, ", ")
}
