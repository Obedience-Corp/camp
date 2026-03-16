package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ktr0731/go-fuzzyfinder"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav/fuzzy"
)

// ResolveCampaignSelection applies the same exact/prefix/name/fuzzy registry
// lookup behavior used by `camp switch`.
func ResolveCampaignSelection(query string, reg *config.Registry, matchWriter io.Writer) (config.RegisteredCampaign, error) {
	c, ok := reg.Get(query)
	if ok {
		return c, nil
	}

	names := reg.List()
	matches := fuzzy.Filter(names, query)
	if len(matches) == 0 {
		return config.RegisteredCampaign{}, fmt.Errorf("campaign %q not found in registry", query)
	}

	bestName := matches[0].Target
	c, ok = reg.GetByName(bestName)
	if !ok {
		return config.RegisteredCampaign{}, fmt.Errorf("campaign %q not found in registry", query)
	}

	if matchWriter != nil {
		_, _ = fmt.Fprintf(matchWriter, "Matched: %s -> %s\n", query, c.Name)
	}

	return c, nil
}

// PickCampaign opens the shared campaign picker UI used by `camp switch`.
func PickCampaign(ctx context.Context, reg *config.Registry) (config.RegisteredCampaign, error) {
	all := reg.ListAll()

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastAccess.After(all[j].LastAccess)
	})

	currentPath, _ := campaign.DetectCached(ctx)

	cfgCache := map[string]*config.CampaignConfig{}
	loadConfig := func(path string) *config.CampaignConfig {
		if cfg, ok := cfgCache[path]; ok {
			return cfg
		}
		cfg, err := config.LoadCampaignConfig(ctx, path)
		if err != nil {
			cfgCache[path] = nil
			return nil
		}
		cfgCache[path] = cfg
		return cfg
	}

	idx, err := fuzzyfinder.Find(
		all,
		func(i int) string {
			c := all[i]
			if c.Path == currentPath {
				return "* " + c.Name
			}
			return "  " + c.Name
		},
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i < 0 || i >= len(all) {
				return ""
			}
			c := all[i]
			cfg := loadConfig(c.Path)
			return formatCampaignPreview(c, cfg, currentPath, w)
		}),
		fuzzyfinder.WithPromptString("Switch to: "),
		fuzzyfinder.WithHeader("  ↑/↓ navigate • type to filter • esc cancel"),
		fuzzyfinder.WithContext(ctx),
	)
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return config.RegisteredCampaign{}, fmt.Errorf("cancelled")
		}
		return config.RegisteredCampaign{}, camperrors.Wrap(err, "picker")
	}

	return all[idx], nil
}

func formatCampaignPreview(c config.RegisteredCampaign, cfg *config.CampaignConfig, currentPath string, w int) string {
	var b strings.Builder
	pad := "  "

	b.WriteString(fmt.Sprintf("%s%s", pad, c.Name))
	if c.Type != "" {
		b.WriteString(fmt.Sprintf("  (%s)", c.Type))
	}
	b.WriteByte('\n')

	if cfg != nil && cfg.Mission != "" {
		b.WriteString(fmt.Sprintf("%s%s\n", pad, cfg.Mission))
	}

	if cfg != nil && cfg.Description != "" {
		b.WriteString(fmt.Sprintf("%s%s\n", pad, cfg.Description))
	}

	if cfg != nil && len(cfg.Projects) > 0 {
		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("%sProjects: (%d)\n", pad, len(cfg.Projects)))
		lineWidth := w - 6
		if lineWidth < 20 {
			lineWidth = 20
		}
		line := pad + "  "
		for i, p := range cfg.Projects {
			name := p.Name
			if i < len(cfg.Projects)-1 {
				name += ", "
			}
			if len(line)+len(name) > lineWidth && line != pad+"  " {
				b.WriteString(line + "\n")
				line = pad + "  "
			}
			line += name
		}
		if line != pad+"  " {
			b.WriteString(line + "\n")
		}
	}

	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("%sPath: %s\n", pad, c.Path))

	if !c.LastAccess.IsZero() {
		b.WriteString(fmt.Sprintf("%sLast: %s\n", pad, c.LastAccess.Format("Jan 2 15:04")))
	}

	if c.Path == currentPath {
		b.WriteString(fmt.Sprintf("\n%s(current)\n", pad))
	}

	return b.String()
}
