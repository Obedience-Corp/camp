// Package promote provides shared logic for promoting intents through the pipeline.
//
// Both the CLI (camp intent promote) and TUI (explorer promote action) use
// this package to ensure consistent behavior.
//
// Pipeline transitions:
//   - TargetReady:    inbox → ready (simple advancement)
//   - TargetFestival: ready → active + create festival
//   - TargetDesign:   ready → active + create design doc
package promote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/intent"
)

// Target identifies the promotion target.
type Target string

const (
	// TargetReady advances an inbox intent to ready status.
	TargetReady Target = "ready"

	// TargetFestival promotes a ready intent to active and creates a festival.
	TargetFestival Target = "festival"

	// TargetDesign promotes a ready intent to active and creates a design doc.
	TargetDesign Target = "design"
)

// Options configures the promote operation.
type Options struct {
	CampaignRoot string
	Target       Target // Promotion target (defaults to TargetFestival for backward compat)
	Force        bool   // Promote even if not in expected status
}

// Result describes the outcome of a promote operation.
type Result struct {
	FestivalName    string        // Slug name passed to fest create
	FestivalDir     string        // Actual directory created by fest (e.g. slug-id)
	FestivalDest    string        // Destination category (planning, ritual, etc.)
	FestivalCreated bool          // True if fest successfully created the festival
	IntentCopied    bool          // True if intent file was copied to ingest
	FestNotFound    bool          // True if fest CLI was not found
	DesignDir       string        // Path to created design doc directory
	DesignCreated   bool          // True if design doc was created
	NewStatus       intent.Status // The status the intent was moved to
}

// festCreateOutput mirrors the JSON output from `fest create festival --json`.
type festCreateOutput struct {
	OK       bool              `json:"ok"`
	Festival map[string]string `json:"festival"`
}

// Promote orchestrates intent promotion based on the target.
//
// Targets and their behavior:
//   - TargetReady:    moves inbox → ready
//   - TargetFestival: moves ready → active, creates festival, sets PromotedTo
//   - TargetDesign:   moves ready → active, creates design doc, sets PromotedTo
func Promote(ctx context.Context, svc *intent.IntentService, i *intent.Intent, opts Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, camperrors.Wrap(err, "context cancelled")
	}

	// Default target for backward compatibility
	target := opts.Target
	if target == "" {
		target = TargetFestival
	}

	switch target {
	case TargetReady:
		return promoteToReady(ctx, svc, i, opts)
	case TargetFestival:
		return promoteToFestival(ctx, svc, i, opts)
	case TargetDesign:
		return promoteToDesign(ctx, svc, i, opts)
	default:
		return Result{}, camperrors.New("unknown promote target: " + string(target))
	}
}

// promoteToReady advances an inbox intent to ready status.
func promoteToReady(ctx context.Context, svc *intent.IntentService, i *intent.Intent, opts Options) (Result, error) {
	if i.Status != intent.StatusInbox && !opts.Force {
		return Result{}, camperrors.New("only inbox intents can be promoted to ready (status: " + i.Status.String() + ")")
	}

	_, err := svc.Move(ctx, i.ID, intent.StatusReady)
	if err != nil {
		return Result{}, camperrors.Wrap(err, "failed to move intent to ready")
	}

	return Result{NewStatus: intent.StatusReady}, nil
}

// promoteToFestival promotes a ready intent to active and creates a festival.
func promoteToFestival(ctx context.Context, svc *intent.IntentService, i *intent.Intent, opts Options) (Result, error) {
	if i.Status != intent.StatusReady && !opts.Force {
		return Result{}, camperrors.New("intent is not ready for promotion (status: " + i.Status.String() + ")")
	}

	// Move to active status (work is beginning, not done).
	moved, err := svc.Move(ctx, i.ID, intent.StatusActive)
	if err != nil {
		return Result{}, camperrors.Wrap(err, "failed to move intent to active")
	}

	// Create festival (best-effort from here on).
	result := createFestival(ctx, opts.CampaignRoot, moved)
	result.NewStatus = intent.StatusActive

	// Set PromotedTo if a festival was created.
	if result.FestivalCreated && result.FestivalDir != "" {
		moved.PromotedTo = result.FestivalDir
		if err := svc.Save(ctx, moved); err != nil {
			return result, camperrors.Wrap(err, "saving promoted_to for festival")
		}
	}

	return result, nil
}

// promoteToDesign promotes a ready intent to active and creates a design doc.
func promoteToDesign(ctx context.Context, svc *intent.IntentService, i *intent.Intent, opts Options) (Result, error) {
	if i.Status != intent.StatusReady && !opts.Force {
		return Result{}, camperrors.New("intent is not ready for promotion (status: " + i.Status.String() + ")")
	}

	// Move to active status.
	moved, err := svc.Move(ctx, i.ID, intent.StatusActive)
	if err != nil {
		return Result{}, camperrors.Wrap(err, "failed to move intent to active")
	}

	// Create design doc.
	designDir, err := createDesignDoc(ctx, opts.CampaignRoot, moved)
	result := Result{NewStatus: intent.StatusActive}
	if err != nil {
		// Best-effort — intent was still moved to active.
		return result, nil
	}

	result.DesignDir = designDir
	result.DesignCreated = true

	// Set PromotedTo.
	moved.PromotedTo = designDir
	if err := svc.Save(ctx, moved); err != nil {
		return result, camperrors.Wrap(err, "saving promoted_to for design doc")
	}

	return result, nil
}

// createDesignDoc creates a design document directory from intent content.
// Returns the relative path to the design directory (e.g. "workflow/design/my-feature").
func createDesignDoc(ctx context.Context, campaignRoot string, i *intent.Intent) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", camperrors.Wrap(err, "context cancelled before creating design doc")
	}

	slug := intent.GenerateSlug(i.Title)
	if slug == "" {
		return "", camperrors.New("could not generate slug from intent title")
	}

	relDir := filepath.Join("workflow", "design", slug)
	absDir := filepath.Join(campaignRoot, relDir)

	if err := os.MkdirAll(absDir, 0755); err != nil {
		return "", camperrors.Wrap(err, "creating design directory")
	}

	// Build README content from intent.
	firstParagraph := ExtractFirstParagraph(i.Content)
	date := time.Now().Format("2006-01-02")

	var content strings.Builder
	content.WriteString("# " + i.Title + "\n\n")
	content.WriteString("## Context\n\n")
	if firstParagraph != "" {
		content.WriteString(firstParagraph + "\n\n")
	}
	content.WriteString("## Status\n\n")
	content.WriteString(fmt.Sprintf("In progress — promoted from intent %s on %s.\n\n", i.ID, date))
	if i.Content != "" {
		content.WriteString("## Content\n\n")
		content.WriteString(strings.TrimSpace(i.Content) + "\n")
	}

	readmePath := filepath.Join(absDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(content.String()), 0644); err != nil {
		return "", camperrors.Wrap(err, "writing design README")
	}

	return relDir, nil
}

// ValidTargetsForStatus returns the valid promote targets for a given status.
func ValidTargetsForStatus(status intent.Status) []Target {
	switch status {
	case intent.StatusInbox:
		return []Target{TargetReady}
	case intent.StatusReady:
		return []Target{TargetFestival, TargetDesign}
	default:
		return nil
	}
}

// createFestival runs `fest create festival --json` and parses the output
// to determine the actual directory name (which includes an ID suffix).
func createFestival(ctx context.Context, campaignRoot string, i *intent.Intent) Result {
	festPath, err := fest.FindFestCLI()
	if err != nil {
		return Result{FestNotFound: true}
	}

	festivalName := intent.GenerateSlug(i.Title)
	festivalGoal := ExtractFirstParagraph(i.Content)

	args := []string{"create", "festival", "--type", "standard", "--name", festivalName, "--json"}
	if festivalGoal != "" {
		args = append(args, "--goal", festivalGoal)
	}

	cmd := exec.CommandContext(ctx, festPath, args...)
	cmd.Dir = campaignRoot
	output, err := cmd.Output()
	if err != nil {
		return Result{FestivalName: festivalName}
	}

	// Parse JSON to get actual directory and dest.
	var festOut festCreateOutput
	if err := json.Unmarshal(output, &festOut); err != nil || !festOut.OK {
		return Result{FestivalName: festivalName}
	}

	dir := festOut.Festival["directory"]
	dest := festOut.Festival["dest"]
	if dir == "" {
		dir = festivalName
	}
	if dest == "" {
		dest = "planning"
	}

	result := Result{
		FestivalName:    festivalName,
		FestivalDir:     dir,
		FestivalDest:    dest,
		FestivalCreated: true,
	}

	// Copy intent file into the festival's ingest directory.
	result.IntentCopied = copyIntentToIngest(campaignRoot, dest, dir, i)

	return result
}

// copyIntentToIngest copies the intent markdown file into the festival's
// 001_INGEST/input_specs/ directory. Returns true on success.
func copyIntentToIngest(campaignRoot, dest, festivalDir string, i *intent.Intent) bool {
	if i.Path == "" {
		return false
	}

	ingestDir := filepath.Join(campaignRoot, "festivals", dest, festivalDir, "001_INGEST", "input_specs")

	if _, err := os.Stat(ingestDir); os.IsNotExist(err) {
		if err := os.MkdirAll(ingestDir, 0755); err != nil {
			return false
		}
	}

	destPath := filepath.Join(ingestDir, filepath.Base(i.Path))
	return copyFile(i.Path, destPath) == nil
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return camperrors.Wrap(err, "opening source file")
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return camperrors.Wrap(err, "creating destination file")
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return camperrors.Wrap(err, "copying file contents")
	}
	return nil
}

// ExtractFirstParagraph returns the first non-header paragraph from markdown content.
func ExtractFirstParagraph(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	paragraphs := strings.Split(content, "\n\n")
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Skip paragraphs that are just markdown headers.
		if strings.HasPrefix(p, "#") {
			lines := strings.SplitN(p, "\n", 2)
			if len(lines) > 1 {
				return strings.TrimSpace(lines[1])
			}
			continue
		}

		return p
	}

	return ""
}
