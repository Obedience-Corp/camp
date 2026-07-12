package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/charmbracelet/huh"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/settings"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// editRegistry is the global registry surface: it lists the registered
// campaigns (name, org, path) from registry.json and routes a selection to its
// safe-edit screen. It never adds, removes, switches, or transfers campaigns —
// those destructive lifecycle operations stay in `camp registry`/`camp org`.
func editRegistry(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	header := "File: " + settings.CatalogPath(e, campaignRoot)
	for {
		reg, err := config.LoadRegistry(ctx)
		if err != nil {
			return camperrors.Wrap(err, "loading registry")
		}

		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(e.Title).
				Description(header).
				Options(registryOptions(reg)...).
				Value(&choice),
		))

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return nil
			}
			return err
		}

		switch choice {
		case valBack, "":
			return nil
		case valSeparator:
			continue
		default:
			if err := editRegistryEntry(ctx, reg.Campaigns[choice], choice, e, campaignRoot); err != nil {
				return err
			}
		}
	}
}

// registryOptions builds a name-sorted picker row per registered campaign, with
// the UUID key as the row value, followed by a separator and Back. Sorting keeps
// the display stable despite random map iteration order.
func registryOptions(reg *config.Registry) []huh.Option[string] {
	uuids := make([]string, 0, len(reg.Campaigns))
	for uuid := range reg.Campaigns {
		uuids = append(uuids, uuid)
	}
	sort.Slice(uuids, func(i, j int) bool {
		a, b := reg.Campaigns[uuids[i]], reg.Campaigns[uuids[j]]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return uuids[i] < uuids[j]
	})

	var opts []huh.Option[string]
	for _, uuid := range uuids {
		c := reg.Campaigns[uuid]
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-24s %-12s %s", c.Name, c.Org, c.Path), uuid))
	}
	return append(opts,
		huh.NewOption(rowSeparator, valSeparator),
		huh.NewOption("Back", valBack),
	)
}

// editRegistryEntry shows the safe-edit form for one campaign: org assignment,
// display rename, and path repair. Path repair only points at an existing
// directory and requires confirmation, so the registry is never left pointing at
// a missing path. No lifecycle operations appear here.
func editRegistryEntry(ctx context.Context, c config.RegisteredCampaign, uuid string, e settings.SettingEntry, campaignRoot string) error {
	name, org, path := c.Name, c.Org, c.Path

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Display name").Value(&name),
		huh.NewInput().Title("Org").Value(&org),
		huh.NewInput().
			Title("Path (repair)").
			Description("Absolute path to the campaign directory").
			Value(&path),
	).Title(c.Name).Description("File: " + settings.CatalogPath(e, campaignRoot)))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return err
	}

	// Registry consumers expect absolute paths; expand CWD-relative input before
	// existence checks so a relative confirm cannot persist.
	normalized, nerr := normalizeRegistryPath(path)
	if nerr != nil {
		return nerr
	}
	path = normalized

	switch classifyPathRepair(c.Path, path, isExistingDir) {
	case pathUnchanged:
		// path == c.Path already; nothing to guard.
	case pathRejected:
		fmt.Println(ui.Warning("Path does not exist or is not a directory; leaving the registry path unchanged."))
		path = c.Path
	case pathNeedsConfirm:
		ok, err := confirmForm(ctx, fmt.Sprintf("Repoint %q to %s?", c.Name, path))
		if err != nil {
			return err
		}
		if !ok {
			path = c.Path
		}
	}

	return saveRegistryEntry(ctx, uuid, applyRegistryEdits(c, name, org, path))
}

// normalizeRegistryPath Abs+Cleans a registry path so relative CWD forms never
// land in registry.json. Empty input is rejected.
func normalizeRegistryPath(path string) (string, error) {
	path = filepath.Clean(path)
	if path == "" || path == "." {
		return "", camperrors.NewValidation("path", "campaign path is required", nil)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", camperrors.Wrap(err, "resolve registry path")
	}
	abs = filepath.Clean(abs)
	if !filepath.IsAbs(abs) {
		return "", camperrors.NewValidation("path", "campaign path must be absolute", nil)
	}
	return abs, nil
}

// pathRepair classifies a proposed registry path change so the interactive guard
// can stay a thin wrapper over pure, testable logic.
type pathRepair int

const (
	pathUnchanged    pathRepair = iota // candidate equals the current path
	pathRejected                       // candidate differs but does not exist as a directory
	pathNeedsConfirm                   // candidate is a different existing directory
)

// classifyPathRepair decides how to treat a proposed path edit. The existence
// check is injected so the decision is unit-testable without touching disk.
func classifyPathRepair(current, candidate string, exists func(string) bool) pathRepair {
	if candidate == current {
		return pathUnchanged
	}
	if !exists(candidate) {
		return pathRejected
	}
	return pathNeedsConfirm
}

// applyRegistryEdits returns a copy of c with the three safe fields updated,
// leaving type, last_access, and status untouched.
func applyRegistryEdits(c config.RegisteredCampaign, name, org, path string) config.RegisteredCampaign {
	c.Name = name
	c.Org = org
	c.Path = path
	return c
}

func isExistingDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// confirmForm asks a yes/no question, defaulting to No. A cancelled prompt is
// treated as No so the caller does not apply the guarded change.
func confirmForm(ctx context.Context, prompt string) (bool, error) {
	var ok bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(prompt).Value(&ok),
	))
	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return false, nil
		}
		return false, err
	}
	return ok, nil
}

// saveRegistryEntry persists a single entry through UpdateRegistry, which holds
// the registry lock for an atomic load-mutate-save and stamps RegistryVersion.
// The UUID key and every other entry are left unchanged. Path uniqueness matches
// Registry.Register: another UUID already owning the path is rejected.
func saveRegistryEntry(ctx context.Context, uuid string, entry config.RegisteredCampaign) error {
	absPath, err := normalizeRegistryPath(entry.Path)
	if err != nil {
		return err
	}
	entry.Path = absPath
	return camperrors.Wrap(config.UpdateRegistry(ctx, func(r *config.Registry) error {
		if _, ok := r.Campaigns[uuid]; !ok {
			return camperrors.Wrap(camperrors.ErrNotFound, "campaign not in registry")
		}
		for id, other := range r.Campaigns {
			if id != uuid && other.Path == entry.Path {
				return camperrors.Wrap(config.ErrPathConflict, id)
			}
		}
		r.Campaigns[uuid] = entry
		return nil
	}), "updating registry")
}
