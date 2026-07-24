package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ktr0731/go-fuzzyfinder"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
)

// switchPickKind distinguishes local registry rows from remote machine rows.
type switchPickKind int

const (
	switchPickLocal switchPickKind = iota
	switchPickRemote
)

// switchPick is a heterogeneous picker candidate. Local rows carry a full
// RegisteredCampaign; remote rows carry machine id + campaign name (and optional
// preview path) and never invent a local Path that would poison registry mutate.
type switchPick struct {
	Kind    switchPickKind
	Local   config.RegisteredCampaign
	Machine string
	Name    string
	Org     string
	Path    string // remote absolute path for preview only
}

// remoteCampaignLoader is the seam the switch picker uses for live fleet
// enumerate. Production uses loadRemoteCampaigns; tests inject a fake fleet.
type remoteCampaignLoader func(ctx context.Context, filter listFilter) ([]campaignEntry, []remoteResult, error)

// pickSwitchTargetOptions controls the interactive switch picker.
type pickSwitchTargetOptions struct {
	Scope  cmdutil.CampaignScope
	Load   remoteCampaignLoader // nil → loadRemoteCampaigns
	Header string
}

// localSwitchPicks builds sorted local candidates (most recently accessed first).
func localSwitchPicks(reg *config.Registry, scope cmdutil.CampaignScope) []switchPick {
	locals := cmdutil.FilterCampaigns(reg, scope)
	sort.Slice(locals, func(i, j int) bool {
		return locals[i].LastAccess.After(locals[j].LastAccess)
	})
	out := make([]switchPick, 0, len(locals))
	for _, c := range locals {
		out = append(out, switchPick{Kind: switchPickLocal, Local: c})
	}
	return out
}

// remoteSwitchPicks converts successful remote campaign rows into picker
// candidates, sorted by machine id then name. Unreachable machines are omitted
// (caller surfaces them in the header).
func remoteSwitchPicks(rows []campaignEntry) []switchPick {
	out := make([]switchPick, 0, len(rows))
	for _, r := range rows {
		if r.Machine == "" || r.Machine == machines.LocalMachineID {
			continue
		}
		out = append(out, switchPick{
			Kind:    switchPickRemote,
			Machine: r.Machine,
			Name:    r.Name,
			Org:     r.Org,
			Path:    r.Path,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Machine != out[j].Machine {
			return out[i].Machine < out[j].Machine
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// unreachableMachineIDs returns machine ids that failed during fan-out.
func unreachableMachineIDs(results []remoteResult) []string {
	var ids []string
	for _, r := range results {
		if r.err != nil {
			ids = append(ids, r.machineID)
		}
	}
	sort.Strings(ids)
	return ids
}

func switchPickLabel(p switchPick, currentPath string, showOrg bool) string {
	prefix := "  "
	switch p.Kind {
	case switchPickRemote:
		return prefix + p.Machine + " · " + p.Name
	default:
		if p.Local.Path == currentPath {
			prefix = "* "
		}
		if showOrg {
			return prefix + p.Local.Org + "/" + p.Local.Name
		}
		return prefix + p.Local.Name
	}
}

func switchPickPreview(p switchPick, cfg *config.CampaignConfig, currentPath string, w int) string {
	if p.Kind == switchPickRemote {
		var b strings.Builder
		pad := "  "
		fmt.Fprintf(&b, "%s%s · %s\n", pad, p.Machine, p.Name)
		if p.Org != "" {
			fmt.Fprintf(&b, "%sOrg: %s\n", pad, p.Org)
		}
		if p.Path != "" {
			fmt.Fprintf(&b, "%sPath: %s\n", pad, p.Path)
		}
		fmt.Fprintf(&b, "\n%s(remote — select to hop via shell-connect)\n", pad)
		return b.String()
	}
	return formatSwitchLocalPreview(p.Local, cfg, currentPath, w)
}

// formatSwitchLocalPreview mirrors cmdutil's campaign preview for local rows
// (kept here so switchPick stays self-contained without exporting cmdutil helpers).
func formatSwitchLocalPreview(c config.RegisteredCampaign, cfg *config.CampaignConfig, currentPath string, w int) string {
	var b strings.Builder
	pad := "  "

	fmt.Fprintf(&b, "%s%s", pad, c.Name)
	if c.Type != "" {
		fmt.Fprintf(&b, "  (%s)", c.Type)
	}
	b.WriteByte('\n')

	if cfg != nil && cfg.Mission != "" {
		fmt.Fprintf(&b, "%s%s\n", pad, cfg.Mission)
	}
	if cfg != nil && cfg.Description != "" {
		fmt.Fprintf(&b, "%s%s\n", pad, cfg.Description)
	}
	if cfg != nil && len(cfg.Projects) > 0 {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%sProjects: (%d)\n", pad, len(cfg.Projects))
		lineWidth := max(w-6, 20)
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
	fmt.Fprintf(&b, "%sPath: %s\n", pad, c.Path)
	if !c.LastAccess.IsZero() {
		fmt.Fprintf(&b, "%sLast: %s\n", pad, c.LastAccess.Format("Jan 2 15:04"))
	}
	if c.Path == currentPath {
		fmt.Fprintf(&b, "\n%s(current)\n", pad)
	}
	return b.String()
}

// pickSwitchTarget opens the interactive switch picker. Locals render immediately;
// when machines are configured, remotes append via go-fuzzyfinder hot reload so
// open never blocks on SSH. Returns error when there are no local campaigns and
// no machines (or remotes-only load yields nothing and was cancelled empty).
func pickSwitchTarget(ctx context.Context, reg *config.Registry, opts pickSwitchTargetOptions) (switchPick, error) {
	loader := opts.Load
	if loader == nil {
		loader = loadRemoteCampaigns
	}

	picks := localSwitchPicks(reg, opts.Scope)
	mf, mfErr := machines.Load()
	hasMachines := mfErr == nil && len(mf.Machines) > 0

	if len(picks) == 0 && !hasMachines {
		return switchPick{}, camperrors.New(fmt.Sprintf("no campaigns found%s", scopeDesc(opts.Scope)))
	}

	// Slice must be addressable for WithHotReloadLock.
	items := picks
	var mu sync.RWMutex
	currentPath, _ := campaign.DetectCached(ctx)
	showOrg := opts.Scope.Org == ""

	header := opts.Header
	if header == "" {
		header = "  ↑/↓ navigate • type to filter • esc cancel"
		if hasMachines {
			header = "  ↑/↓ navigate • type to filter • remotes loading… • esc cancel"
		}
	}

	cfgCache := map[string]*config.CampaignConfig{}
	loadConfig := func(path string) *config.CampaignConfig {
		if path == "" {
			return nil
		}
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

	if hasMachines {
		go func() {
			rows, results, err := loader(ctx, listFilterFromScope(opts.Scope))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				// Keep locals; surface failure only in header isn't possible mid-flight
				// without a second channel — remotes simply don't append.
				return
			}
			remotes := remoteSwitchPicks(rows)
			items = append(items, remotes...)
			if unreach := unreachableMachineIDs(results); len(unreach) > 0 {
				// Header is fixed at open; unreachable info lives in previews only.
				// Locals remain selectable regardless.
				_ = unreach
			}
		}()
	}

	findOpts := []fuzzyfinder.Option{
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			mu.RLock()
			defer mu.RUnlock()
			if i < 0 || i >= len(items) {
				return ""
			}
			p := items[i]
			var cfg *config.CampaignConfig
			if p.Kind == switchPickLocal {
				cfg = loadConfig(p.Local.Path)
			}
			return switchPickPreview(p, cfg, currentPath, w)
		}),
		fuzzyfinder.WithPromptString("Switch to: "),
		fuzzyfinder.WithHeader(header),
		fuzzyfinder.WithContext(ctx),
	}
	if hasMachines {
		findOpts = append(findOpts, fuzzyfinder.WithHotReloadLock(mu.RLocker()))
	}

	var slice any
	if hasMachines {
		slice = &items
	} else {
		slice = items
	}

	idx, err := fuzzyfinder.Find(
		slice,
		func(i int) string {
			// itemFunc is locked by fuzzyfinder under hot reload — do not lock.
			p := items[i]
			return switchPickLabel(p, currentPath, showOrg)
		},
		findOpts...,
	)
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return switchPick{}, camperrors.Newf("cancelled")
		}
		return switchPick{}, camperrors.Wrap(err, "picker")
	}

	mu.RLock()
	defer mu.RUnlock()
	if idx < 0 || idx >= len(items) {
		return switchPick{}, camperrors.New("picker selection out of range")
	}
	return items[idx], nil
}

func scopeDesc(scope cmdutil.CampaignScope) string {
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
