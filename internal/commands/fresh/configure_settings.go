package fresh

import (
	"github.com/Obedience-Corp/camp/internal/config"
)

// settingOptionsFor builds the choices the settings editor offers for a step.
// A project scope gains an inherit option, since clearing the key there is a
// real third outcome. The global scope has no one to inherit from, but bool
// keys still need a third choice: "default" clears the key so the built-in
// applies, which is distinct from writing an explicit true/false.
func settingOptionsFor(cfg *config.FreshConfig, projectName string, step freshWorkflowStep) []freshSettingOption {
	project := projectName != ""

	if step.Setting == freshSettingBranch {
		options := make([]freshSettingOption, 0, 3)
		if project {
			options = append(options, freshSettingOption{
				label:  "inherit from global · " + branchSummary(cfg.Branch),
				action: freshSettingInherit,
			})
		}
		return append(options,
			freshSettingOption{label: "no branch · stay on the default branch", action: freshSettingNoBranch},
			freshSettingOption{label: "create a branch...", action: freshSettingCustomBranch},
		)
	}

	options := make([]freshSettingOption, 0, 3)
	switch {
	case project && !step.GlobalOnly:
		options = append(options, freshSettingOption{
			label:  "inherit from global · " + onOffWord(globalBoolValue(cfg, step.Setting)),
			action: freshSettingInherit,
		})
	case !project:
		// Built-in defaults for prune / prune_remote / push_upstream are true.
		// Name the default, not the currently resolved value, so an explicit
		// "off" does not make the default option read "default · off".
		options = append(options, freshSettingOption{
			label:  "default · " + onOffWord(builtInBoolDefault(step.Setting)),
			action: freshSettingInherit,
		})
	}
	return append(options,
		freshSettingOption{label: "on", action: freshSettingOn},
		freshSettingOption{label: "off", action: freshSettingOff},
	)
}

// currentSettingAction is the option that matches what the selected scope
// stores today, so the editor opens on the current answer rather than on a
// default that would silently rewrite the key if the user just pressed enter.
//
// For global bools this must inspect the stored pointer, not the resolved
// value: Resolve* collapses a missing key to the built-in default (true), and
// mapping that to "on" would open the editor on an option that writes an
// explicit true into a previously absent key.
func currentSettingAction(cfg *config.FreshConfig, projectName string, step freshWorkflowStep) freshSettingAction {
	pc, hasProject := cfg.Projects[projectName]

	switch step.Setting {
	case freshSettingBranch:
		if projectName != "" {
			if !hasProject || pc.Branch == nil {
				return freshSettingInherit
			}
			if *pc.Branch == "" {
				return freshSettingNoBranch
			}
			return freshSettingCustomBranch
		}
		if cfg.Branch == "" {
			return freshSettingNoBranch
		}
		return freshSettingCustomBranch
	case freshSettingPushUpstream:
		if projectName != "" {
			if !hasProject || pc.PushUpstream == nil {
				return freshSettingInherit
			}
			return boolAction(*pc.PushUpstream)
		}
		if cfg.PushUpstream == nil {
			return freshSettingInherit
		}
		return boolAction(*cfg.PushUpstream)
	case freshSettingPrune:
		if cfg.Prune == nil {
			return freshSettingInherit
		}
		return boolAction(*cfg.Prune)
	case freshSettingPruneRemote:
		if cfg.PruneRemote == nil {
			return freshSettingInherit
		}
		return boolAction(*cfg.PruneRemote)
	}
	return freshSettingInherit
}

// builtInBoolDefault is the value Resolve* uses when a global bool key is
// absent. Kept local so option labels do not re-derive it from a currently
// written override.
func builtInBoolDefault(setting freshSettingKey) bool {
	switch setting {
	case freshSettingPushUpstream, freshSettingPrune, freshSettingPruneRemote:
		return true
	default:
		return false
	}
}

// settingScopeBranch is the branch this scope stores on its own, used to seed
// the text input when the editor opens.
//
// It deliberately does not fall back to the global branch. Seeding the field
// with an inherited value puts text in it that the user never typed and cannot
// tell apart from their own: switching to "create a branch" and typing then
// appends, which silently produced branch names like "developfeat/storefront".
// A scope with no branch of its own opens on an empty field.
func settingScopeBranch(cfg *config.FreshConfig, projectName string) string {
	if projectName == "" {
		return cfg.Branch
	}
	if pc, ok := cfg.Projects[projectName]; ok && pc.Branch != nil {
		return *pc.Branch
	}
	return ""
}

func globalBoolValue(cfg *config.FreshConfig, setting freshSettingKey) bool {
	switch setting {
	case freshSettingPushUpstream:
		return cfg.ResolveFreshPushUpstream("")
	case freshSettingPrune:
		return cfg.ResolveFreshPrune()
	case freshSettingPruneRemote:
		return cfg.ResolveFreshPruneRemote()
	}
	return false
}

func boolAction(on bool) freshSettingAction {
	if on {
		return freshSettingOn
	}
	return freshSettingOff
}

func onOffWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func branchSummary(branch string) string {
	if branch == "" {
		return "no branch"
	}
	return branch
}

// settingTitle names the fresh.yaml key a settings row edits, so the editor
// header matches what the user would search for in the file.
func settingTitle(setting freshSettingKey) string {
	switch setting {
	case freshSettingPrune:
		return "prune"
	case freshSettingPruneRemote:
		return "prune_remote"
	case freshSettingBranch:
		return "branch"
	case freshSettingPushUpstream:
		return "push_upstream"
	}
	return ""
}

func parseSettingKey(name string) (freshSettingKey, bool) {
	switch name {
	case "prune":
		return freshSettingPrune, true
	case "prune_remote":
		return freshSettingPruneRemote, true
	case "branch":
		return freshSettingBranch, true
	case "push_upstream":
		return freshSettingPushUpstream, true
	default:
		return freshSettingNone, false
	}
}

func parseSettingAction(name string) (freshSettingAction, bool) {
	switch name {
	case "inherit":
		return freshSettingInherit, true
	case "on":
		return freshSettingOn, true
	case "off":
		return freshSettingOff, true
	case "no-branch":
		return freshSettingNoBranch, true
	case "branch":
		return freshSettingCustomBranch, true
	default:
		return 0, false
	}
}

func settingActionName(action freshSettingAction) string {
	switch action {
	case freshSettingInherit:
		return "inherit"
	case freshSettingOn:
		return "on"
	case freshSettingOff:
		return "off"
	case freshSettingNoBranch:
		return "no-branch"
	case freshSettingCustomBranch:
		return "branch"
	default:
		return ""
	}
}
