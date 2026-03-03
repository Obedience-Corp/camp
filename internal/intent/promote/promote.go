// Package promote provides shared logic for promoting intents to festivals.
//
// Both the CLI (camp intent promote) and TUI (explorer promote action) use
// this package to ensure consistent behavior: move to done, create festival
// via fest CLI, copy intent to ingest, and set PromotedTo.
package promote

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/intent"
)

// Options configures the promote operation.
type Options struct {
	CampaignRoot string
	Force        bool // Promote even if not in ready status
}

// Result describes the outcome of a promote operation.
type Result struct {
	FestivalName    string // Slug name passed to fest create
	FestivalDir     string // Actual directory created by fest (e.g. slug-id)
	FestivalDest    string // Destination category (planning, ritual, etc.)
	FestivalCreated bool   // True if fest successfully created the festival
	IntentCopied    bool   // True if intent file was copied to ingest
	FestNotFound    bool   // True if fest CLI was not found
}

// festCreateOutput mirrors the JSON output from `fest create festival --json`.
type festCreateOutput struct {
	OK       bool              `json:"ok"`
	Festival map[string]string `json:"festival"`
}

// Promote orchestrates the full intent-to-festival promotion:
//  1. Validates the intent status (must be ready unless Force is set)
//  2. Moves the intent to done status
//  3. Creates a festival via fest CLI (best-effort)
//  4. Copies the intent file into the festival's ingest directory
//  5. Sets intent.PromotedTo and saves
func Promote(ctx context.Context, svc *intent.IntentService, i *intent.Intent, opts Options) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, camperrors.Wrap(err, "context cancelled")
	}

	if i.Status != intent.StatusReady && !opts.Force {
		return Result{}, camperrors.New("intent is not ready for promotion (status: " + i.Status.String() + ")")
	}

	// Move to done status.
	moved, err := svc.Move(ctx, i.ID, intent.StatusDone)
	if err != nil {
		return Result{}, camperrors.Wrap(err, "failed to move intent to done")
	}

	// Create festival (best-effort from here on).
	result := createFestival(ctx, opts.CampaignRoot, moved)

	// Set PromotedTo if a festival was created.
	if result.FestivalCreated && result.FestivalDir != "" {
		moved.PromotedTo = result.FestivalDir
		_ = svc.Save(ctx, moved)
	}

	return result, nil
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
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
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
