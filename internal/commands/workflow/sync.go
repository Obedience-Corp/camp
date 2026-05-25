package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
)

// syncAction is one planned mutation derived from an auto-fixable finding.
type syncAction struct {
	Finding finding  `json:"finding"`
	Kind    string   `json:"kind"`
	Target  string   `json:"target"`
	Kept    string   `json:"kept,omitempty"`
	Removed []string `json:"removed,omitempty"`
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
	state := newSyncState(ctx, cmd, campaignRoot, cfg, len(actions))
	return state.apply(actions)
}

type syncState struct {
	ctx          context.Context
	cmd          *cobra.Command
	campaignRoot string
	cfg          *config.CampaignConfig
	jumps        *config.JumpsConfig

	jumpsDirty    bool
	conceptsDirty bool
	applied       []syncAction
}

func newSyncState(ctx context.Context, cmd *cobra.Command, campaignRoot string, cfg *config.CampaignConfig, capacity int) *syncState {
	jumps := cfg.Jumps
	if jumps == nil {
		defaults := config.DefaultJumpsConfig()
		jumps = &defaults
	}
	if jumps.Shortcuts == nil {
		jumps.Shortcuts = make(map[string]config.ShortcutConfig)
	}
	cfg.Jumps = jumps

	return &syncState{
		ctx:          ctx,
		cmd:          cmd,
		campaignRoot: campaignRoot,
		cfg:          cfg,
		jumps:        jumps,
		applied:      make([]syncAction, 0, capacity),
	}
}

func (s *syncState) apply(actions []syncAction) ([]syncAction, error) {
	for _, action := range actions {
		applied, ok, err := s.applyAction(action)
		if err != nil {
			return nil, err
		}
		if ok {
			s.applied = append(s.applied, applied)
		}
	}

	if err := s.save(); err != nil {
		return nil, err
	}
	s.invalidateCacheIfNeeded()

	return s.applied, nil
}

func (s *syncState) applyAction(action syncAction) (syncAction, bool, error) {
	switch action.Kind {
	case syncRemoveShortcut:
		return action, s.removeShortcut(action.Target), nil
	case syncRemoveConcept:
		return action, s.removeConcept(action.Target), nil
	case syncAddConcept:
		return action, s.addConcept(action.Target), nil
	case syncDeleteNavCache:
		if err := navindex.Delete(s.campaignRoot); err != nil {
			return action, false, camperrors.Wrap(err, "delete nav cache")
		}
		return action, true, nil
	case syncDeduplicateShortcut:
		return s.deduplicateShortcut(action)
	default:
		return action, false, nil
	}
}

func (s *syncState) removeShortcut(key string) bool {
	if _, ok := s.jumps.Shortcuts[key]; !ok {
		return false
	}
	delete(s.jumps.Shortcuts, key)
	s.jumpsDirty = true
	return true
}

func (s *syncState) removeConcept(name string) bool {
	concepts := s.currentConcepts()
	next := make([]config.ConceptEntry, 0, len(concepts))
	removed := false
	for _, c := range concepts {
		if strings.EqualFold(c.Name, name) {
			removed = true
			continue
		}
		next = append(next, c)
	}
	if !removed {
		return false
	}
	s.cfg.ConceptList = next
	s.conceptsDirty = true
	return true
}

func (s *syncState) addConcept(target string) bool {
	rel := strings.TrimRight(target, "/") + "/"
	typeName := workflowTypeFromPath(rel)
	if typeName == "" {
		return false
	}
	concepts := s.currentConcepts()
	for _, c := range concepts {
		if strings.EqualFold(c.Name, typeName) {
			return false
		}
	}
	s.cfg.ConceptList = append(concepts, config.ConceptEntry{
		Name:        typeName,
		Path:        rel,
		Description: typeName + " workflow",
	})
	s.conceptsDirty = true
	return true
}

func (s *syncState) deduplicateShortcut(action syncAction) (syncAction, bool, error) {
	normalized := action.Target
	keys := matchingShortcutKeys(s.jumps.Shortcuts, normalized)
	if len(keys) <= 1 {
		return action, false, nil
	}

	keptEntry, hasNormalized := s.jumps.Shortcuts[normalized]
	if !hasNormalized {
		keptEntry = s.jumps.Shortcuts[keys[0]]
	}

	removed := make([]string, 0, len(keys))
	for _, key := range keys {
		if key != normalized {
			removed = append(removed, key)
		}
		delete(s.jumps.Shortcuts, key)
	}
	s.jumps.Shortcuts[normalized] = keptEntry

	action.Kept = normalized
	action.Removed = removed
	s.jumpsDirty = true
	return action, true, nil
}

func (s *syncState) currentConcepts() []config.ConceptEntry {
	concepts := s.cfg.ConceptList
	if len(concepts) == 0 {
		concepts = s.cfg.Concepts()
	}
	return concepts
}

func (s *syncState) save() error {
	if s.jumpsDirty {
		if err := config.SaveJumpsConfig(s.ctx, s.campaignRoot, s.jumps); err != nil {
			return err
		}
	}
	if s.conceptsDirty {
		if err := config.SaveCampaignConfig(s.ctx, s.campaignRoot, s.cfg); err != nil {
			return err
		}
	}
	return nil
}

func (s *syncState) invalidateCacheIfNeeded() {
	if !(s.jumpsDirty || s.conceptsDirty) {
		return
	}
	for _, a := range s.applied {
		switch a.Kind {
		case syncDeleteNavCache:
			return
		}
	}
	invalidateNavigationCache(s.cmd, s.campaignRoot)
}

func emitSyncHuman(w io.Writer, actions, applied []syncAction, apply bool) {
	if len(actions) == 0 {
		fmt.Fprintln(w, "sync: nothing to fix")
		return
	}
	if apply {
		fmt.Fprintf(w, "sync: applied %d / %d auto-fixable findings\n", len(applied), len(actions))
		for _, a := range applied {
			fmt.Fprintf(w, "  fixed %s (%s)\n", a.Finding.Code, syncActionDetail(a))
		}
		return
	}
	fmt.Fprintf(w, "sync: would fix %d auto-fixable findings (re-run with --apply)\n", len(actions))
	for _, a := range actions {
		fmt.Fprintf(w, "  plan  %s (%s)\n", a.Finding.Code, a.Target)
	}
}

func syncActionDetail(a syncAction) string {
	if a.Kind != syncDeduplicateShortcut || a.Kept == "" {
		return a.Target
	}
	detail := a.Target + "; kept " + a.Kept
	if len(a.Removed) > 0 {
		detail += "; removed " + strings.Join(a.Removed, ", ")
	}
	return detail
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
