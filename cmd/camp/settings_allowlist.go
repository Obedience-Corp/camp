package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/charmbracelet/huh"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/settings"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// allowlistAddValue and the "cmd:" prefix keep command rows from colliding with
// the Add/separator/Back sentinels regardless of what a command is named.
const (
	allowlistAddValue  = "\x00add"
	allowlistCmdPrefix = "cmd:"
)

// editAllowlist edits .campaign/settings/allowlist.json: toggle allowed per
// command, add commands, and remove commands. inherit_defaults is shown but not
// changed here, so it is never silently flipped.
func editAllowlist(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	for {
		al, err := config.LoadAllowlist(ctx, campaignRoot)
		if err != nil {
			return camperrors.Wrap(err, "loading allowlist")
		}

		desc := fmt.Sprintf("File: %s (inherit_defaults: %v)", settings.CatalogPath(e, campaignRoot), al.InheritDefaults)
		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(e.Title).
				Description(desc).
				Options(allowlistOptions(al)...).
				Value(&choice),
		))

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return nil
			}
			return err
		}

		switch {
		case choice == valBack || choice == "":
			return nil
		case choice == valSeparator:
			continue
		case choice == allowlistAddValue:
			if err := addAllowlistEntry(ctx, campaignRoot, e); err != nil {
				return err
			}
		case strings.HasPrefix(choice, allowlistCmdPrefix):
			if err := editAllowlistCommand(ctx, campaignRoot, strings.TrimPrefix(choice, allowlistCmdPrefix)); err != nil {
				return err
			}
		}
	}
}

// allowlistOptions renders one row per command (checkbox for allowed state,
// value = "cmd:<name>"), name-sorted, followed by Add / separator / Back.
func allowlistOptions(al *config.Allowlist) []huh.Option[string] {
	names := sortedCommandNames(al)
	var opts []huh.Option[string]
	for _, name := range names {
		c := al.Commands[name]
		label := fmt.Sprintf("[%s] %s", checkbox(c.Allowed), name)
		if c.Description != "" {
			label += "  " + c.Description
		}
		opts = append(opts, huh.NewOption(label, allowlistCmdPrefix+name))
	}
	return append(opts,
		huh.NewOption("Add command", allowlistAddValue),
		huh.NewOption(rowSeparator, valSeparator),
		huh.NewOption("Back", valBack),
	)
}

func sortedCommandNames(al *config.Allowlist) []string {
	names := make([]string, 0, len(al.Commands))
	for name := range al.Commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func checkbox(b bool) string {
	if b {
		return "x"
	}
	return " "
}

// editAllowlistCommand toggles or removes a single command.
func editAllowlistCommand(ctx context.Context, campaignRoot, name string) error {
	al, err := config.LoadAllowlist(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "loading allowlist")
	}
	c, ok := al.Commands[name]
	if !ok {
		return nil
	}

	toggleLabel := "Set allowed"
	if c.Allowed {
		toggleLabel = "Set not allowed"
	}
	var action string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(name).
			Description(fmt.Sprintf("allowed: %v", c.Allowed)).
			Options(
				huh.NewOption(toggleLabel, "toggle"),
				huh.NewOption("Remove command", "remove"),
				huh.NewOption("Back", valBack),
			).
			Value(&action),
	))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return err
	}

	switch action {
	case "toggle":
		setCommandAllowed(al, name, !c.Allowed)
		return saveAllowlist(ctx, campaignRoot, al)
	case "remove":
		removeAllowlistCommand(al, name)
		return saveAllowlist(ctx, campaignRoot, al)
	default:
		return nil
	}
}

// addAllowlistEntry prompts for a new command and appends it.
func addAllowlistEntry(ctx context.Context, campaignRoot string, e settings.SettingEntry) error {
	var name, desc string
	allowed := true
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Command name").Value(&name),
		huh.NewConfirm().Title("Allowed?").Value(&allowed),
		huh.NewInput().Title("Description").Value(&desc),
	).Title("Add command").Description("File: " + settings.CatalogPath(e, campaignRoot)))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return err
	}

	al, err := config.LoadAllowlist(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "loading allowlist")
	}
	if err := addAllowlistCommand(al, name, allowed, desc); err != nil {
		fmt.Println(ui.Warning("Command not added: " + err.Error()))
		return nil
	}
	return saveAllowlist(ctx, campaignRoot, al)
}

// setCommandAllowed, removeAllowlistCommand, and addAllowlistCommand mutate only
// the Commands map, leaving Version and InheritDefaults untouched.
func setCommandAllowed(al *config.Allowlist, name string, allowed bool) {
	if c, ok := al.Commands[name]; ok {
		c.Allowed = allowed
		al.Commands[name] = c
	}
}

func removeAllowlistCommand(al *config.Allowlist, name string) {
	delete(al.Commands, name)
}

func addAllowlistCommand(al *config.Allowlist, name string, allowed bool, desc string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return camperrors.NewValidation("command", "command name is required", nil)
	}
	if al.Commands == nil {
		al.Commands = map[string]config.AllowlistCommand{}
	}
	if _, exists := al.Commands[name]; exists {
		return camperrors.NewValidation("command", fmt.Sprintf("%q already exists", name), nil)
	}
	al.Commands[name] = config.AllowlistCommand{Allowed: allowed, Description: strings.TrimSpace(desc)}
	return nil
}

func saveAllowlist(ctx context.Context, campaignRoot string, al *config.Allowlist) error {
	if err := config.SaveAllowlist(ctx, campaignRoot, al); err != nil {
		return camperrors.Wrap(err, "saving allowlist")
	}
	return nil
}
