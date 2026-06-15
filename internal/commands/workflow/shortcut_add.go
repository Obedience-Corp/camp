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
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/nav"
)

func newShortcutCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shortcut",
		Short: "Manage navigation shortcuts for workflow collections",
		Long: `Manage navigation shortcuts for custom workflow collections.

Workflow shortcuts are stored in campaign configuration and point to
workflow/<type>/ directories. Use subcommands to attach or repair shortcut
entries after creating or moving workflow collections.`,
	}
	cmd.AddCommand(newShortcutAddCommand())
	return cmd
}

func newShortcutAddCommand() *cobra.Command {
	var replace, jsonOut bool
	cmd := &cobra.Command{
		Use:   "add <type> <key>",
		Short: "Attach a navigation shortcut to an existing workflow",
		Long: `Attach a navigation shortcut to an existing workflow collection.

The command updates .campaign/settings/jumps.yaml so cgo and camp navigation
can jump to workflow/<type>/ by key. The workflow type must already exist. Use
--replace to overwrite a conflicting shortcut and --json for machine-readable
result details.`,
		Args: jsoncontract.Args(JSONSchemaVersion, func() bool { return jsonOut }, cobra.ExactArgs(2)),
		RunE: jsoncontract.RunE(JSONSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			return runShortcutAdd(cmd.Context(), cmd, args[0], args[1], replace, jsonOut)
		}),
	}
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(JSONSchemaVersion, func() bool { return jsonOut }))
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an existing shortcut with the same name")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func runShortcutAdd(ctx context.Context, cmd *cobra.Command, typeName, key string, replace, jsonOut bool) error {
	if err := validatePathSegment("type", typeName); err != nil {
		return err
	}
	if err := validatePathSegment("shortcut", key); err != nil {
		return err
	}

	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	entries, err := enumerateWorkflowEntries(campaignRoot, cfg)
	if err != nil {
		return err
	}
	var entry *workflowEntry
	for i := range entries {
		if strings.EqualFold(entries[i].Type, typeName) {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		if jsonOut {
			return camperrors.NewNotFound("workflow", typeName, nil)
		}
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "unknown workflow type: %s\n", typeName); err != nil {
			return err
		}
		return errWorkflowNotFound
	}

	shortcutKey := nav.NormalizeNavigationName(key)
	noChange := entry.HasShortcut && nav.NormalizeNavigationName(entry.ShortcutKey) == shortcutKey && entry.ShortcutPath == entry.Path

	if !noChange {
		if err := upsertShortcut(ctx, campaignRoot, cfg, shortcutKey, entry.Path, titleOrType(*entry), replace); err != nil {
			return err
		}
		invalidateNavigationCache(cmd, campaignRoot)
	}

	if jsonOut {
		return emitShortcutAddJSON(cmd.OutOrStdout(), entry.Type, shortcutKey, entry.Path, noChange)
	}
	return emitShortcutAddHuman(cmd.OutOrStdout(), entry.Type, shortcutKey, entry.Path, noChange)
}

func titleOrType(e workflowEntry) string {
	if e.Title != "" {
		return e.Title
	}
	return e.Type
}

func emitShortcutAddHuman(w io.Writer, typeName, key, path string, noChange bool) error {
	if noChange {
		_, err := fmt.Fprintf(w, "no changes for shortcut %s -> %s\n", key, path)
		return err
	}
	_, err := fmt.Fprintf(w, "shortcut added: %s -> %s (workflow %s)\n", key, path, typeName)
	return err
}

func emitShortcutAddJSON(w io.Writer, typeName, key, path string, noChange bool) error {
	out := struct {
		SchemaVersion string    `json:"schema_version"`
		GeneratedAt   time.Time `json:"generated_at"`
		Type          string    `json:"type"`
		Shortcut      string    `json:"shortcut"`
		Path          string    `json:"path"`
		NoChanges     bool      `json:"no_changes"`
		Applied       bool      `json:"applied"`
	}{
		SchemaVersion: JSONSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Type:          typeName,
		Shortcut:      key,
		Path:          path,
		NoChanges:     noChange,
		Applied:       !noChange,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
