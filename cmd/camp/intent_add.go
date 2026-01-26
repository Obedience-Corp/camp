package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/editor"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/project"
	"github.com/obediencecorp/camp/internal/ui/theme"
)

var intentAddCmd = &cobra.Command{
	Use:   "add [title]",
	Short: "Create a new intent",
	Long: `Create a new intent with fast or deep capture mode.

CAPTURE MODES:
  Fast (default)    Quick huh form → direct file creation
  Deep (--edit)     Full template in editor → validate → save

Fast capture is optimized for speed - ideas are saved immediately after
the huh form with minimal overhead. Use --edit when you need the full
template and want to add body content.

Examples:
  camp intent add                        Interactive form
  camp intent add "Add dark mode"        Fast capture with title
  camp intent add -e "Complex feature"   Deep capture with editor
  camp intent add -t feature "New API"   Set type explicitly`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIntentAdd,
}

func init() {
	intentCmd.AddCommand(intentAddCmd)

	flags := intentAddCmd.Flags()
	flags.StringP("type", "t", "idea", "Intent type (idea, feature, bug, research, chore)")
	flags.StringP("project", "p", "", "Related project")
	flags.BoolP("edit", "e", false, "Open in $EDITOR for deep capture")
	flags.String("priority", "medium", "Priority level (low, medium, high)")
	flags.String("horizon", "later", "Time horizon (now, next, later)")
	flags.StringSlice("blocked-by", nil, "Blocking dependencies")
	flags.StringSlice("depends-on", nil, "Prerequisites")
}

func runIntentAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	intentType, _ := cmd.Flags().GetString("type")
	projectName, _ := cmd.Flags().GetString("project")
	useEditor, _ := cmd.Flags().GetBool("edit")

	// Get title from args or empty for form
	var title string
	if len(args) > 0 {
		title = args[0]
	}

	// Find campaign root first (needed for project list)
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create path resolver
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)

	// Collect input via huh form if needed
	var body string
	title, intentType, projectName, body, err = collectIntentInput(ctx, campaignRoot, title, intentType, projectName)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return fmt.Errorf("intent creation cancelled")
		}
		return fmt.Errorf("failed to collect input: %w", err)
	}

	// Create service
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Build create options
	opts := intent.CreateOptions{
		Title:   title,
		Type:    intent.Type(intentType),
		Concept: projectName, // projectName from CLI maps to Concept field
		Body:    body,
	}

	// Determine capture mode
	if useEditor {
		return runDeepCapture(ctx, svc, opts)
	}

	return runFastCapture(ctx, svc, opts)
}

// collectIntentInput displays the huh form if title is not provided.
// Returns title, intentType, project, body, error.
func collectIntentInput(ctx context.Context, campaignRoot, title, intentType, projectName string) (string, string, string, string, error) {
	var body string

	// Skip form if title already provided (CLI fast path)
	if title != "" {
		if intentType == "" {
			intentType = "idea"
		}
		return title, intentType, projectName, body, nil
	}

	// Default values
	if intentType == "" {
		intentType = "idea"
	}

	// Load project list for selector
	projects, err := project.List(ctx, campaignRoot)
	if err != nil {
		return "", "", "", "", fmt.Errorf("loading projects: %w", err)
	}

	// Build project options: "None" first, then actual projects
	projectOptions := []huh.Option[string]{
		huh.NewOption("None", ""),
	}
	for _, p := range projects {
		projectOptions = append(projectOptions, huh.NewOption(p.Name, p.Name))
	}

	// DEBUG: Test with all 4 fields (original form)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Title").
				Description("What's the intent? (required)").
				Placeholder("Add dark mode toggle...").
				Value(&title).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("title is required")
					}
					return nil
				}),

			huh.NewSelect[string]().
				Title("Type").
				Description("Category of intent").
				Options(
					huh.NewOption("Idea", "idea"),
					huh.NewOption("Feature", "feature"),
					huh.NewOption("Bug", "bug"),
					huh.NewOption("Research", "research"),
					huh.NewOption("Chore", "chore"),
				).
				Value(&intentType),

			huh.NewSelect[string]().
				Title("Project").
				Description("Related project").
				Options(projectOptions...).
				Value(&projectName),

			huh.NewText().
				Title("Description").
				Description("What is this intent about? (required)").
				Placeholder("Start typing here...").
				Value(&body).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("description is required")
					}
					return nil
				}),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		return "", "", "", "", err
	}

	return title, intentType, projectName, body, nil
}

// runFastCapture creates intent file directly without editor.
func runFastCapture(ctx context.Context, svc *intent.IntentService, opts intent.CreateOptions) error {
	result, err := svc.CreateDirect(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to create intent: %w", err)
	}

	fmt.Printf("✓ Intent created: %s\n", result.Path)
	return nil
}

// runDeepCapture opens editor for full template expansion.
func runDeepCapture(ctx context.Context, svc *intent.IntentService, opts intent.CreateOptions) error {
	// Use editor function from editor package
	editorFn := func(ctx context.Context, path string) error {
		return editor.Edit(ctx, path)
	}

	result, err := svc.CreateWithEditor(ctx, opts, editorFn)
	if err != nil {
		if errors.Is(err, intent.ErrCancelled) {
			return fmt.Errorf("intent creation cancelled")
		}
		return fmt.Errorf("failed to create intent: %w", err)
	}

	fmt.Printf("✓ Intent created: %s\n", result.Path)
	return nil
}
