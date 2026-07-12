package main

import "github.com/spf13/cobra"

const (
	manifestDefaultRestrictedReason = "not approved for non-interactive agent execution"
	manifestCommandGroupReason      = "command group; use an annotated subcommand"
	manifestInteractiveReason       = "requires interactive terminal"
)

var manifestAgentAllowedReasons = map[string]string{
	"cache info":                 "Read-only cache metadata",
	"concepts":                   "Read-only concept listing",
	"create":                     "Non-interactive with -d and -m; interactive fallback otherwise",
	"doctor":                     "Read path (--json) is safe; never pass --fix from an agent",
	"dungeon list":               "Read-only dungeon listing",
	"dungeon move":               "Non-interactive move with explicit arguments",
	"flow add":                   "Non-interactive flow registration",
	"flow history":               "Read-only workflow transition history",
	"flow items":                 "Read-only workflow item listing",
	"flow list":                  "Read-only flow listing",
	"flow show":                  "Read-only flow structure display",
	"flow status":                "Read-only flow statistics",
	"gather design":              "Non-interactive with explicit selectors and --title; interactive picker otherwise",
	"go":                         "Non-interactive path resolution when used with explicit arguments",
	"id":                         "Read-only campaign ID output",
	"intent add":                 "Programmatic create path is safe with title/body flags; agents must not use TUI-only flags",
	"intent count":               "Read-only intent count",
	"intent find":                "Read-only intent search",
	"intent list":                "Read-only intent listing",
	"intent show":                "Read-only intent detail",
	"log":                        "Read-only git log",
	"machine add":                "Explicit id/host/auth args (or --discover with --yes/an id) are non-interactive",
	"machine diagnose":           "Read path (socket status) is safe; never pass --reset from an agent",
	"machine list":               "Read-only listing of ~/.obey/machines.yaml",
	"machine remove":             "Non-interactive removal with explicit id argument",
	"project list":               "Read-only project listing",
	"project remote list":        "Read-only project remote listing",
	"promote":                    "Routes to a type-specific promote; needs an explicit id and --target in non-interactive use",
	"quest archive":              "Non-interactive quest archive with explicit selector",
	"quest checklist":            "Read-only quest checklist listing",
	"quest complete":             "Non-interactive quest completion with explicit selector",
	"quest create":               "Non-interactive quest creation with explicit arguments",
	"quest item add":             "Non-interactive checklist item creation",
	"quest item done":            "Non-interactive checklist item completion",
	"quest item edit":            "Non-interactive checklist item edit",
	"quest item link-workitem":   "Non-interactive workitem link on a checklist item",
	"quest item rank":            "Non-interactive checklist item reordering",
	"quest item reopen":          "Non-interactive checklist item reopen",
	"quest item unlink-workitem": "Non-interactive workitem unlink on a checklist item",
	"quest link":                 "Non-interactive quest link operation",
	"quest links":                "Read-only quest link listing",
	"quest list":                 "Read-only quest listing",
	"quest pause":                "Non-interactive quest pause with explicit selector",
	"quest rename":               "Non-interactive quest rename with explicit selector",
	"quest restore":              "Non-interactive quest restore with explicit selector",
	"quest resume":               "Non-interactive quest resume with explicit selector",
	"quest show":                 "Read-only quest detail",
	"quest status":               "Read-only terminal quest context with --json",
	"quest unlink":               "Non-interactive quest unlink operation",
	"quest update":               "Non-interactive quest metadata update with explicit selector",
	"registry check":             "Read-only registry integrity report",
	"root":                       "Read-only campaign root output",
	"settings get":               "Read-only settings output with --json support",
	"settings set":               "Non-interactive settings mutation with validated values",
	"shelve":                     "Non-interactive shelving with explicit arguments",
	"skills link":                "Non-interactive skill link operation",
	"skills status":              "Read-only skill status",
	"skills unlink":              "Non-interactive skill unlink operation",
	"status":                     "Read-only git status",
	"status all":                 "Read-only multi-repository status",
	"switch":                     "Non-interactive with explicit campaign argument",
	"version":                    "Read-only version output",
	"workflow doctor":            "Read-only workflow health report",
	"workflow list":              "Read-only workflow listing",
	"workflow show":              "Read-only workflow detail",
	"workitem":                   "Supports --json for non-interactive output",
	"workitem commit":            "Non-interactive commit planning and execution with explicit selector",
	"workitem commits":           "Read-only commit history lookup",
	"workitem create":            "Non-interactive workitem creation with explicit slug",
	"workitem current":           "Non-interactive current-workitem selection and query",
	"workitem doctor":            "Read path (--json) is safe; never pass --fix from an agent",
	"workitem link":              "Non-interactive workitem link operation",
	"workitem links":             "Read-only workitem link listing",
	"workitem group":             "Non-interactive group update with explicit selector",
	"workitem priority":          "Non-interactive priority update with explicit selector",
	"workitem promote":           "Non-interactive promote fully specified by --target and flags",
	"workitem resolve":           "Read-only workitem context resolution",
	"workitem stage":             "Non-interactive attention-stage update with explicit selector",
	"workitem unlink":            "Non-interactive workitem unlink operation",
	"worktrees info":             "Read-only worktree metadata",
	"worktrees list":             "Read-only worktree listing",
}

var manifestAllowedInteractivePaths = map[string]bool{
	"create":        true,
	"gather design": true,
	"intent add":    true,
	"switch":        true,
}

var manifestInteractivePaths = map[string]bool{
	"dungeon crawl":  true,
	"intent crawl":   true,
	"intent explore": true,
	"settings":       true,
}

func init() {
	applyManifestAnnotations(rootCmd, "")
}

func applyManifestAnnotations(cmd *cobra.Command, prefix string) {
	for _, child := range cmd.Commands() {
		path := child.Name()
		if prefix != "" {
			path = prefix + " " + child.Name()
		}
		if skipManifestCommand(path, child) {
			continue
		}
		ensureManifestAnnotation(child, path)
		applyManifestAnnotations(child, path)
	}
}

func ensureManifestAnnotation(cmd *cobra.Command, path string) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	if reason, ok := manifestAgentAllowedReasons[path]; ok {
		cmd.Annotations["agent_allowed"] = "true"
		cmd.Annotations["agent_reason"] = reason
		if manifestAllowedInteractivePaths[path] {
			cmd.Annotations["interactive"] = "true"
		}
		return
	}
	if manifestInteractivePaths[path] {
		cmd.Annotations["agent_allowed"] = "false"
		cmd.Annotations["agent_reason"] = manifestInteractiveReason
		cmd.Annotations["interactive"] = "true"
		return
	}
	if _, ok := cmd.Annotations["agent_allowed"]; !ok {
		cmd.Annotations["agent_allowed"] = "false"
		if cmd.HasAvailableSubCommands() && !cmd.Runnable() {
			cmd.Annotations["agent_reason"] = manifestCommandGroupReason
		} else {
			cmd.Annotations["agent_reason"] = manifestDefaultRestrictedReason
		}
	}
	if cmd.Annotations["agent_reason"] == "" {
		cmd.Annotations["agent_reason"] = manifestDefaultRestrictedReason
	}
}

func skipManifestCommand(path string, cmd *cobra.Command) bool {
	if cmd.Hidden {
		return true
	}
	return path == "help" || path == "completion" ||
		path == "completion bash" ||
		path == "completion fish" ||
		path == "completion powershell" ||
		path == "completion zsh"
}
