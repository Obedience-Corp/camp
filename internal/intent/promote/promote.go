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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	promotecore "github.com/Obedience-Corp/camp/internal/promote"
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
	FestCLIError    string        // Stderr from a failed fest CLI invocation
	DesignDir       string        // Path to created design doc directory
	DesignCreated   bool          // True if design doc was created
	NewStatus       intent.Status // The status the intent was moved to
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

	moved, err := svc.Move(ctx, i.ID, intent.StatusActive)
	if err != nil {
		return Result{}, camperrors.Wrap(err, "failed to move intent to active")
	}

	name := intent.SlugFromTitle(moved.Title)
	goal := promotecore.ExtractFirstParagraph(moved.Content)
	fr, _ := promotecore.FindAndCreateFestival(ctx, opts.CampaignRoot, name, goal)

	result := Result{
		FestivalName:    fr.Name,
		FestivalDir:     fr.Dir,
		FestivalDest:    fr.Dest,
		FestivalCreated: fr.Created,
		FestNotFound:    fr.NotFound,
		FestCLIError:    fr.CLIError,
		NewStatus:       intent.StatusActive,
	}

	if fr.Created {
		result.IntentCopied = promotecore.CopyIntoFestivalIngest(opts.CampaignRoot, fr.Dest, fr.Dir, moved.Path)
	}

	if fr.Created && fr.Dir != "" {
		moved.PromotedTo = fr.Dir
		if err := promotecore.RecordPromotion(fr.Dir, func(promotecore.PromotionRecord) error {
			return svc.Save(ctx, moved)
		}); err != nil {
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

	// Create design doc first for transactional semantics — if this fails, intent
	// remains in ready and no status transition occurs.
	designDir, createdNow, err := createDesignDoc(ctx, opts.CampaignRoot, i)
	if err != nil {
		return Result{}, camperrors.Wrap(err, "failed to create design doc")
	}

	// Move to active status only after design doc creation succeeded.
	moved, err := svc.Move(ctx, i.ID, intent.StatusActive)
	if err != nil {
		// Best-effort rollback of newly created design artifacts.
		if createdNow {
			_ = removeCreatedDesignDoc(opts.CampaignRoot, designDir)
		}
		return Result{}, camperrors.Wrap(err, "failed to move intent to active")
	}

	// Set PromotedTo.
	moved.PromotedTo = designDir
	if err := svc.Save(ctx, moved); err != nil {
		rollbackErr := rollbackIntentToReady(ctx, svc, i.ID)
		if createdNow {
			_ = removeCreatedDesignDoc(opts.CampaignRoot, designDir)
		}
		if rollbackErr != nil {
			return Result{}, camperrors.Wrapf(err, "saving promoted_to for design doc (rollback failed: %v)", rollbackErr)
		}
		return Result{}, camperrors.Wrap(err, "saving promoted_to for design doc")
	}

	return Result{
		NewStatus:     intent.StatusActive,
		DesignDir:     designDir,
		DesignCreated: true,
	}, nil
}

// createDesignDoc creates a design document directory from intent content.
// Returns the relative path to the design directory (e.g. "workflow/design/my-feature").
func createDesignDoc(ctx context.Context, campaignRoot string, i *intent.Intent) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, camperrors.Wrap(err, "context cancelled before creating design doc")
	}

	slug := intent.SlugFromTitle(i.Title)
	if slug == "" {
		return "", false, camperrors.New("could not generate slug from intent title")
	}

	dirName := slug
	if suffix := intentIDTimestampSuffix(i.ID); suffix != "" {
		dirName = slug + "-" + suffix
	}

	relDir := filepath.Join("workflow", "design", dirName)
	absDir := filepath.Join(campaignRoot, relDir)

	if err := os.MkdirAll(filepath.Dir(absDir), 0755); err != nil {
		return "", false, camperrors.Wrap(err, "creating design parent directory")
	}

	createdNow := false
	if err := os.Mkdir(absDir, 0755); err == nil {
		createdNow = true
	} else if os.IsExist(err) {
		info, statErr := os.Stat(absDir)
		if statErr != nil {
			return "", false, camperrors.Wrap(statErr, "checking design directory")
		}
		if !info.IsDir() {
			return "", false, camperrors.New("design path exists and is not a directory: " + absDir)
		}
	} else {
		return "", false, camperrors.Wrap(err, "creating design directory")
	}

	// Build README content from intent.
	firstParagraph := promotecore.ExtractFirstParagraph(i.Content)
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
	if _, err := os.Stat(readmePath); err == nil {
		// Do not overwrite an existing design doc.
		return relDir, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, camperrors.Wrap(err, "checking design README")
	}

	if err := os.WriteFile(readmePath, []byte(content.String()), 0644); err != nil {
		return "", false, camperrors.Wrap(err, "writing design README")
	}

	return relDir, createdNow, nil
}

func rollbackIntentToReady(ctx context.Context, svc *intent.IntentService, id string) error {
	_, err := svc.Move(ctx, id, intent.StatusReady)
	return err
}

func removeCreatedDesignDoc(campaignRoot, relDir string) error {
	if relDir == "" {
		return nil
	}

	absDir := filepath.Clean(filepath.Join(campaignRoot, relDir))
	designRoot := filepath.Clean(filepath.Join(campaignRoot, "workflow", "design"))
	prefix := designRoot + string(os.PathSeparator)
	if absDir != designRoot && !strings.HasPrefix(absDir, prefix) {
		return camperrors.New("refusing to remove path outside workflow/design")
	}
	return os.RemoveAll(absDir)
}

// intentIDTimestampSuffix extracts the trailing YYYYMMDD-HHMMSS timestamp from
// a generated intent ID. Returns empty string when unavailable.
func intentIDTimestampSuffix(id string) string {
	const suffixLen = len("20060102-150405")
	if len(id) < suffixLen {
		return ""
	}

	suffix := id[len(id)-suffixLen:]
	for i, ch := range suffix {
		if i == 8 {
			if ch != '-' {
				return ""
			}
			continue
		}
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return suffix
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
