package initcmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// festivalsDir is the campaign-root-relative directory fest init populates.
const festivalsDir = "festivals"

// commitInitialScaffold makes the first commit of a freshly initialized
// campaign so a new workspace starts with its scaffold in git instead of
// leaving the user to write the commit camp already knows how to describe.
//
// It stages a selective file list rather than everything: camp init inside an
// existing repository must not fold unrelated working-tree changes into the
// campaign's first commit. The commit is synchronous because the user is
// waiting on init itself and a fresh repository with only a queued commit is
// not a finished workspace. Failure is reported, never fatal: the scaffold is
// on disk either way.
func commitInitialScaffold(ctx context.Context, initResult *scaffold.InitResult, skillPaths []string, festRan bool, w Writers) {
	files, err := git.FilterIgnored(ctx, initResult.CampaignRoot, buildInitCommitFiles(initResult, skillPaths, festRan))
	if err != nil {
		writef(w.HumanOut, "\n%s initial commit skipped: %v\n", ui.WarningIcon(), err)
		return
	}
	if len(files) == 0 {
		return
	}

	cfg, err := config.LoadCampaignConfig(ctx, initResult.CampaignRoot)
	if err != nil {
		writef(w.HumanOut, "\n%s initial commit skipped: %v\n", ui.WarningIcon(), err)
		return
	}

	result := commit.Init(ctx, commit.InitOptions{
		Options: commit.Options{
			CampaignRoot:  initResult.CampaignRoot,
			CampaignID:    cfg.ID,
			CampaignName:  cfg.Name,
			Files:         files,
			SelectiveOnly: true,
			Synchronous:   true,
		},
		Description: buildInitCommitMessage(initResult, skillPaths, festRan),
	})

	switch {
	case result.Committed:
		writef(w.HumanOut, "\n%s Initial commit created\n", ui.SuccessIcon())
	case result.Err != nil:
		writef(w.HumanOut, "\n%s initial commit failed: %v\n", ui.WarningIcon(), result.Err)
		writeLine(w.HumanOut, ui.Dim("The scaffold is on disk; run 'camp commit' to record it."))
	case result.Message != "":
		writef(w.HumanOut, "\n%s %s\n", ui.InfoIcon(), result.Message)
	}
}

// buildInitCommitFiles returns the campaign-root-relative paths the scaffold
// produced, including projected skill links and the festivals tree only when
// this invocation ran fest init (a pre-existing festivals/ tree is the user's,
// not the scaffold's). The caller drops gitignored entries (worktrees, ledger events)
// before staging, since git add refuses an explicitly named ignored path.
func buildInitCommitFiles(initResult *scaffold.InitResult, skillPaths []string, festRan bool) []string {
	files := make([]string, 0, len(initResult.FilesCreated)+len(initResult.FilesModified)+len(initResult.DirsCreated)+len(skillPaths)+1)
	files = append(files, initResult.FilesCreated...)
	files = append(files, initResult.FilesModified...)
	files = append(files, initResult.DirsCreated...)
	files = append(files, skillPaths...)
	if festRan {
		files = append(files, festivalsDir)
	}
	return commit.NormalizeFiles(initResult.CampaignRoot, files...)
}

// buildInitCommitMessage describes the scaffold in the commit body so the first
// commit reads as a record of what camp init produced. Paths are campaign-root
// relative; directories are counted rather than listed because git tracks the
// files inside them, not the directories themselves.
func buildInitCommitMessage(initResult *scaffold.InitResult, skillPaths []string, festRan bool) string {
	var b strings.Builder
	root := initResult.CampaignRoot

	if n := len(initResult.DirsCreated); n > 0 {
		fmt.Fprintf(&b, "Directories created: %d\n\n", n)
	}

	writeSection := func(title string, paths []string) {
		rel := commit.NormalizeFiles(root, paths...)
		if len(rel) == 0 {
			return
		}
		b.WriteString(title + ":\n")
		for _, p := range rel {
			fmt.Fprintf(&b, "  - %s\n", p)
		}
		b.WriteString("\n")
	}
	writeSection("Files created", initResult.FilesCreated)
	writeSection("Files updated", initResult.FilesModified)
	writeSection("Skill links projected", skillPaths)

	if festRan {
		fmt.Fprintf(&b, "Festival Methodology initialized in %s/\n", festivalsDir)
	}

	return strings.TrimRight(b.String(), "\n")
}
