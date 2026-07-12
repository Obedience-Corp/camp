package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/charmbracelet/huh"
	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/editor"
	"github.com/Obedience-Corp/camp/internal/settings"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// editCampaignManifest is the sub-menu for editing .campaign/campaign.yaml. Each
// action loads the current config, applies its edit, and persists through
// SaveCampaignConfig, so fields outside the edited area are preserved.
func editCampaignManifest(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	header := "File: " + settings.CatalogPath(e, campaignRoot)
	for {
		options := []huh.Option[string]{
			huh.NewOption("Identity, mission, and type", "scalars"),
			huh.NewOption("Intent tags", "tags"),
			huh.NewOption("Concepts taxonomy (opens editor)", "concepts"),
			huh.NewOption(rowSeparator, valSeparator),
			huh.NewOption("Back", valBack),
		}

		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(e.Title).
				Description(header).
				Options(options...).
				Value(&choice),
		))

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				return nil
			}
			return err
		}

		switch choice {
		case valBack:
			return nil
		case valSeparator:
			continue
		case "scalars":
			if err := editCampaignScalars(ctx, e, campaignRoot); err != nil {
				return err
			}
		case "tags":
			if err := editIntentTags(ctx, e, campaignRoot); err != nil {
				return err
			}
		case "concepts":
			if err := editConceptsViaEditor(ctx, e, campaignRoot); err != nil {
				return err
			}
		}
	}
}

// campaignScalars holds the editable scalar fields of campaign.yaml, bridged to
// plain strings for the huh form (Type is a CampaignType alias on disk).
type campaignScalars struct {
	Name        string
	Description string
	Mission     string
	Type        string
	CommitCmd   string
}

func campaignScalarsFrom(cfg *config.CampaignConfig) campaignScalars {
	return campaignScalars{
		Name:        cfg.Name,
		Description: cfg.Description,
		Mission:     cfg.Mission,
		Type:        string(cfg.Type),
		CommitCmd:   cfg.Hooks.CommitMessage.Command,
	}
}

// applyCampaignScalars writes the edited scalars back onto cfg, leaving every
// other field (id, created_at, projects, concepts, intents) untouched.
func applyCampaignScalars(cfg *config.CampaignConfig, s campaignScalars) {
	cfg.Name = s.Name
	cfg.Description = s.Description
	cfg.Mission = s.Mission
	cfg.Type = config.CampaignType(s.Type)
	cfg.Hooks.CommitMessage.Command = s.CommitCmd
}

func editCampaignScalars(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "loading campaign.yaml")
	}

	s := campaignScalarsFrom(cfg)
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Name").Value(&s.Name),
		huh.NewText().Title("Description").Value(&s.Description),
		huh.NewText().Title("Mission").Value(&s.Mission),
		huh.NewSelect[string]().
			Title("Type").
			Options(
				huh.NewOption("product", string(config.CampaignTypeProduct)),
				huh.NewOption("research", string(config.CampaignTypeResearch)),
				huh.NewOption("tools", string(config.CampaignTypeTools)),
				huh.NewOption("personal", string(config.CampaignTypePersonal)),
			).
			Value(&s.Type),
		huh.NewInput().Title("Commit message hook").Value(&s.CommitCmd),
	).Title(e.Title).Description("File: " + settings.CatalogPath(e, campaignRoot)))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return err
	}

	applyCampaignScalars(cfg, s)
	return saveCampaignManifest(ctx, campaignRoot, cfg)
}

// parseTagLines splits newline-separated tag input into a trimmed, de-duplicated
// list, preserving the order the user entered them.
func parseTagLines(joined string) []string {
	var tags []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(joined, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		tags = append(tags, t)
	}
	return tags
}

func editIntentTags(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "loading campaign.yaml")
	}

	joined := strings.Join(cfg.Intents.Tags, "\n")
	form := huh.NewForm(huh.NewGroup(
		huh.NewText().
			Title("Intent tags (one per line)").
			Description("File: " + settings.CatalogPath(e, campaignRoot)).
			Value(&joined),
	))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil
		}
		return err
	}

	cfg.Intents.Tags = parseTagLines(joined)
	return saveCampaignManifest(ctx, campaignRoot, cfg)
}

const conceptsEditorHeader = `# Edit the campaign concept taxonomy below (YAML list of concepts).
# Each concept needs a name. A group-only parent may omit path but must have
# children. Invalid YAML or a missing name is rejected and campaign.yaml is
# left unchanged.
`

// validateConcepts checks an edited concept tree before it is written. Each
// entry needs a name, and needs either a path or children (a pure-parent node
// may omit its path). It recurses into children.
func validateConcepts(entries []config.ConceptEntry) error {
	return validateConceptEntries(entries, "concepts")
}

func validateConceptEntries(entries []config.ConceptEntry, loc string) error {
	for i, e := range entries {
		where := fmt.Sprintf("%s[%d]", loc, i)
		if strings.TrimSpace(e.Name) == "" {
			return camperrors.NewValidation(where, "concept name is required", nil)
		}
		if strings.TrimSpace(e.Path) == "" && len(e.Children) == 0 {
			return camperrors.NewValidation(fmt.Sprintf("%s (%s)", where, e.Name), "concept needs a path or children", nil)
		}
		if err := validateConceptEntries(e.Children, where+".children"); err != nil {
			return err
		}
	}
	return nil
}

// editConceptsViaEditor round-trips the concepts subtree through $EDITOR. It is
// the single explicit editor exception (DP2): the edit is all-or-nothing, so an
// invalid or unparseable result never touches campaign.yaml.
func editConceptsViaEditor(ctx context.Context, e settings.SettingEntry, campaignRoot string) error {
	if !ui.IsTerminal() {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "editing concepts requires an interactive terminal")
	}

	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "loading campaign.yaml")
	}

	data, err := yaml.Marshal(cfg.ConceptList)
	if err != nil {
		return camperrors.Wrap(err, "serializing concepts")
	}

	tmpPath, err := writeConceptsTempFile(data)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()

	if err := editor.Edit(ctx, tmpPath); err != nil {
		return camperrors.Wrap(err, "running editor")
	}

	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		return camperrors.Wrap(err, "reading edited concepts")
	}

	var edited []config.ConceptEntry
	if err := yaml.Unmarshal(raw, &edited); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Concepts not saved: invalid YAML: %v", err)))
		return nil
	}
	if err := validateConcepts(edited); err != nil {
		fmt.Println(ui.Warning(fmt.Sprintf("Concepts not saved: %v", err)))
		return nil
	}

	cfg.ConceptList = edited
	return saveCampaignManifest(ctx, campaignRoot, cfg)
}

func writeConceptsTempFile(data []byte) (string, error) {
	tmp, err := os.CreateTemp("", "camp-concepts-*.yaml")
	if err != nil {
		return "", camperrors.Wrap(err, "creating temp file")
	}
	if _, err := tmp.WriteString(conceptsEditorHeader); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", camperrors.Wrap(err, "writing temp file")
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", camperrors.Wrap(err, "writing temp file")
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", camperrors.Wrap(err, "closing temp file")
	}
	return tmp.Name(), nil
}

// saveCampaignManifest persists campaign.yaml edits through SaveCampaignConfig,
// the canonical writer (atomic write, preserved ordering, hooks placeholder).
func saveCampaignManifest(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig) error {
	if err := config.SaveCampaignConfig(ctx, campaignRoot, cfg); err != nil {
		return camperrors.Wrap(err, "saving campaign.yaml")
	}
	return nil
}
