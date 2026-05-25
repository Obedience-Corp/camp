package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
)

// syncAction is one planned mutation derived from an auto-fixable finding.
type syncAction struct {
	Finding finding `json:"finding"`
	Kind    string  `json:"kind"`
	Target  string  `json:"target"`
}

const (
	syncRemoveShortcut      = "remove_shortcut"
	syncRemoveConcept       = "remove_concept"
	syncAddConcept          = "add_concept"
	syncDeleteNavCache      = "delete_nav_cache"
	syncDeduplicateShortcut = "deduplicate_shortcut"
)

func newSyncCommand() *cobra.Command {
	var apply, jsonOut bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Repair auto-fixable doctor findings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), cmd, apply, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "perform writes (default: report only)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runSync(ctx context.Context, cmd *cobra.Command, apply, jsonOut bool) error {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	findings, err := collectFindings(campaignRoot, cfg)
	if err != nil {
		return err
	}

	actions := planSyncActions(findings)
	var applied []syncAction
	if apply && len(actions) > 0 {
		applied, err = applySyncActions(ctx, cmd, campaignRoot, cfg, actions)
		if err != nil {
			return err
		}
	}

	if jsonOut {
		return emitSyncJSON(cmd.OutOrStdout(), findings, actions, applied, apply)
	}
	emitSyncHuman(cmd.OutOrStdout(), actions, applied, apply)
	return nil
}

func planSyncActions(findings []finding) []syncAction {
	var actions []syncAction
	for _, f := range findings {
		if !f.AutoFixable {
			continue
		}
		switch f.Code {
		case codeShortcutMissingTarget:
			actions = append(actions, syncAction{Finding: f, Kind: syncRemoveShortcut, Target: strings.TrimPrefix(f.Target, "shortcut:")})
		case codeConceptMissingDir:
			actions = append(actions, syncAction{Finding: f, Kind: syncRemoveConcept, Target: strings.TrimPrefix(f.Target, "concept:")})
		case codeDirMissingConcept:
			actions = append(actions, syncAction{Finding: f, Kind: syncAddConcept, Target: strings.TrimPrefix(f.Target, "dir:")})
		case codeCacheStale:
			actions = append(actions, syncAction{Finding: f, Kind: syncDeleteNavCache, Target: "cache:nav"})
		case codeShortcutDuplicate:
			actions = append(actions, syncAction{Finding: f, Kind: syncDeduplicateShortcut, Target: strings.TrimPrefix(f.Target, "shortcut:")})
		}
	}
	return actions
}

func applySyncActions(ctx context.Context, cmd *cobra.Command, campaignRoot string, cfg *config.CampaignConfig, actions []syncAction) ([]syncAction, error) {
	jumps := cfg.Jumps
	if jumps == nil {
		defaults := config.DefaultJumpsConfig()
		jumps = &defaults
	}
	if jumps.Shortcuts == nil {
		jumps.Shortcuts = make(map[string]config.ShortcutConfig)
	}
	cfg.Jumps = jumps

	jumpsDirty := false
	conceptsDirty := false
	applied := make([]syncAction, 0, len(actions))

	for _, action := range actions {
		switch action.Kind {
		case syncRemoveShortcut:
			key := action.Target
			if _, ok := jumps.Shortcuts[key]; ok {
				delete(jumps.Shortcuts, key)
				jumpsDirty = true
				applied = append(applied, action)
			}

		case syncRemoveConcept:
			name := action.Target
			concepts := cfg.ConceptList
			if len(concepts) == 0 {
				concepts = cfg.Concepts()
			}
			next := concepts[:0]
			removed := false
			for _, c := range concepts {
				if strings.EqualFold(c.Name, name) {
					removed = true
					continue
				}
				next = append(next, c)
			}
			if removed {
				cfg.ConceptList = next
				conceptsDirty = true
				applied = append(applied, action)
			}

		case syncAddConcept:
			rel := strings.TrimRight(action.Target, "/") + "/"
			typeName := workflowTypeFromPath(rel)
			if typeName == "" {
				continue
			}
			concepts := cfg.ConceptList
			if len(concepts) == 0 {
				concepts = cfg.Concepts()
			}
			already := false
			for _, c := range concepts {
				if strings.EqualFold(c.Name, typeName) {
					already = true
					break
				}
			}
			if !already {
				cfg.ConceptList = append(concepts, config.ConceptEntry{
					Name:        typeName,
					Path:        rel,
					Description: typeName + " workflow",
				})
				conceptsDirty = true
				applied = append(applied, action)
			}

		case syncDeleteNavCache:
			if err := navindex.Delete(campaignRoot); err != nil {
				return nil, camperrors.Wrap(err, "delete nav cache")
			}
			applied = append(applied, action)

		case syncDeduplicateShortcut:
			normalized := action.Target
			keys := make([]string, 0)
			for k := range jumps.Shortcuts {
				if nav.NormalizeNavigationName(k) == normalized {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			if len(keys) <= 1 {
				continue
			}
			kept := keys[0]
			for _, k := range keys[1:] {
				delete(jumps.Shortcuts, k)
			}
			if normalized != kept {
				entry := jumps.Shortcuts[kept]
				delete(jumps.Shortcuts, kept)
				jumps.Shortcuts[normalized] = entry
			}
			jumpsDirty = true
			applied = append(applied, action)
		}
	}

	if jumpsDirty {
		if err := config.SaveJumpsConfig(ctx, campaignRoot, jumps); err != nil {
			return nil, err
		}
	}
	if conceptsDirty {
		if err := config.SaveCampaignConfig(ctx, campaignRoot, cfg); err != nil {
			return nil, err
		}
	}

	hasCacheDelete := false
	for _, a := range applied {
		if a.Kind == syncDeleteNavCache {
			hasCacheDelete = true
			break
		}
	}
	if !hasCacheDelete && (jumpsDirty || conceptsDirty) {
		invalidateNavigationCache(cmd, campaignRoot)
	}

	return applied, nil
}

func emitSyncHuman(w io.Writer, actions, applied []syncAction, apply bool) {
	if len(actions) == 0 {
		fmt.Fprintln(w, "sync: nothing to fix")
		return
	}
	if apply {
		fmt.Fprintf(w, "sync: applied %d / %d auto-fixable findings\n", len(applied), len(actions))
		for _, a := range applied {
			fmt.Fprintf(w, "  fixed %s (%s)\n", a.Finding.Code, a.Target)
		}
		return
	}
	fmt.Fprintf(w, "sync: would fix %d auto-fixable findings (re-run with --apply)\n", len(actions))
	for _, a := range actions {
		fmt.Fprintf(w, "  plan  %s (%s)\n", a.Finding.Code, a.Target)
	}
}

func emitSyncJSON(w io.Writer, findings []finding, planned, applied []syncAction, apply bool) error {
	if planned == nil {
		planned = []syncAction{}
	}
	if applied == nil {
		applied = []syncAction{}
	}
	out := struct {
		SchemaVersion string       `json:"schema_version"`
		GeneratedAt   time.Time    `json:"generated_at"`
		Findings      []finding    `json:"findings"`
		Planned       []syncAction `json:"planned"`
		Applied       []syncAction `json:"applied"`
		Apply         bool         `json:"apply"`
	}{
		SchemaVersion: JSONSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Findings:      findings,
		Planned:       planned,
		Applied:       applied,
		Apply:         apply,
	}
	if out.Findings == nil {
		out.Findings = []finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

