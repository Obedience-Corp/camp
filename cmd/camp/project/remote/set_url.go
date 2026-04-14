package remote

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectRemoteSetURLCmd = &cobra.Command{
	Use:   "set-url <url>",
	Short: "Update a remote URL for the project",
	Long: `Update a remote URL across all tracked locations with automatic rollback.

For submodule projects, updates three locations in order:
  1. .gitmodules  (canonical, tracked in git)
  2. local git submodule config (.git/config of the campaign root)
  3. remote config inside the project repo

If any step fails, previous steps are automatically rolled back to keep
all locations consistent. If rollback also fails, recovery instructions
are printed so you can fix it manually.

For non-submodule projects, only the remote config is updated.

Flags:
  --name      Remote name to update (default: origin)
  --no-verify Skip connectivity check after updating
  --no-stage  Skip auto-staging .gitmodules

Examples:
  camp project remote set-url git@github.com:org/new-name.git
  camp project remote set-url https://github.com/org/repo.git --name upstream
  camp project remote set-url git@github.com:org/repo.git --no-verify`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectRemoteSetURL,
}

func init() {
	Cmd.AddCommand(projectRemoteSetURLCmd)

	projectRemoteSetURLCmd.Flags().StringP("name", "n", "origin", "Remote name to update")
	projectRemoteSetURLCmd.Flags().Bool("no-verify", false, "Skip connectivity check after updating")
	projectRemoteSetURLCmd.Flags().Bool("no-stage", false, "Skip auto-staging .gitmodules")
}

// setURLState tracks which mutations have been applied so rollback knows what to undo.
type setURLState struct {
	campRoot      string
	submodulePath string
	remoteName    string
	projectPath   string

	oldDeclaredURL string // original .gitmodules URL (empty if non-submodule)
	oldRemoteURL   string // original remote URL in the project

	gitmodulesUpdated bool
	syncCompleted     bool
	remoteURLUpdated  bool

	steps []string
}

func (s *setURLState) addStep(msg string) {
	s.steps = append(s.steps, msg)
}

// rollback undoes completed mutations in reverse order.
// Returns nil if rollback succeeds, or recovery instructions if it fails.
func (s *setURLState) rollback(ctx context.Context) []string {
	var failures []string

	// Undo step 3: restore project remote URL
	if s.remoteURLUpdated && s.oldRemoteURL != "" {
		if err := git.SetRemoteURL(ctx, s.projectPath, s.remoteName, s.oldRemoteURL); err != nil {
			failures = append(failures,
				"# Restore project remote URL:",
				fmt.Sprintf("  git -C %s remote set-url %s %s", s.projectPath, s.remoteName, s.oldRemoteURL),
			)
		}
	}

	// Undo step 1: restore .gitmodules
	if s.gitmodulesUpdated && s.oldDeclaredURL != "" {
		if err := git.SetDeclaredURL(ctx, s.campRoot, s.submodulePath, s.oldDeclaredURL); err != nil {
			failures = append(failures,
				"# Restore .gitmodules URL:",
				fmt.Sprintf("  git -C %s config -f .gitmodules submodule.%s.url %s",
					s.campRoot, s.submodulePath, s.oldDeclaredURL),
			)
		} else if s.syncCompleted {
			// Re-sync so .git/config matches the restored .gitmodules
			if err := git.SyncSubmodule(ctx, s.campRoot, s.submodulePath); err != nil {
				failures = append(failures,
					"# Re-sync submodule config:",
					fmt.Sprintf("  git -C %s submodule sync -- %s", s.campRoot, s.submodulePath),
				)
			}
		}
	}

	return failures
}

func runProjectRemoteSetURL(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	newURL := args[0]

	remoteName, _ := cmd.Flags().GetString("name")
	noVerify, _ := cmd.Flags().GetBool("no-verify")
	noStage, _ := cmd.Flags().GetBool("no-stage")

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	resolved, err := project.Resolve(ctx, campRoot, flagRemoteProject)
	if err != nil {
		return err
	}
	if err := resolved.RequireGit("git remotes"); err != nil {
		return err
	}

	isSubmodule := resolved.Source == project.SourceSubmodule
	submodulePath := resolved.LogicalPath

	state := &setURLState{
		campRoot:      campRoot,
		submodulePath: submodulePath,
		remoteName:    remoteName,
		projectPath:   resolved.Path,
	}

	// Capture BEFORE state for display and rollback
	fmt.Printf("Updating remote %s for project %s\n\n",
		ui.Value(remoteName), ui.Value(resolved.Name))

	if isSubmodule {
		state.oldDeclaredURL, _ = git.GetDeclaredURL(ctx, campRoot, submodulePath)
		if state.oldDeclaredURL != "" {
			fmt.Printf("  %s %s\n", ui.Dim("before (.gitmodules):"), ui.Dim(state.oldDeclaredURL))
		}
	}

	remotesBefore, _ := git.ListRemotes(ctx, resolved.Path)
	for _, r := range remotesBefore {
		if r.Name == remoteName {
			state.oldRemoteURL = r.FetchURL
			fmt.Printf("  %s %s\n", ui.Dim("before (remote):      "), ui.Dim(r.FetchURL))
			break
		}
	}
	fmt.Println()

	// Step 1: Update .gitmodules (submodule only, lock-susceptible)
	if isSubmodule {
		setURLErr := git.WithLockRetry(ctx, campRoot, git.SubmoduleRetryConfig(), func() error {
			return git.SetDeclaredURL(ctx, campRoot, submodulePath, newURL)
		})
		if setURLErr != nil {
			return fmt.Errorf("update .gitmodules: %w", setURLErr)
		}
		state.gitmodulesUpdated = true
		state.addStep("updated .gitmodules")
		fmt.Printf("  %s Updated .gitmodules\n", ui.SuccessIcon())
	}

	// Step 2: Sync submodule config (submodule only, lock-susceptible)
	if isSubmodule {
		syncErr := git.WithLockRetry(ctx, campRoot, git.SubmoduleRetryConfig(), func() error {
			return git.SyncSubmodule(ctx, campRoot, submodulePath)
		})
		if syncErr != nil {
			fmt.Printf("  %s Sync failed, rolling back .gitmodules...\n", ui.WarningIcon())
			if failures := state.rollback(ctx); len(failures) > 0 {
				printRecoveryInstructions(failures)
			} else {
				fmt.Printf("  %s Rollback succeeded — no changes were applied\n", ui.SuccessIcon())
			}
			return fmt.Errorf("sync submodule config: %w", syncErr)
		}
		state.syncCompleted = true
		state.addStep("synced local submodule config")
		fmt.Printf("  %s Synced local submodule config\n", ui.SuccessIcon())
	}

	// Step 3: Update remote URL inside the project repo
	if err := git.SetRemoteURL(ctx, resolved.Path, remoteName, newURL); err != nil {
		if isSubmodule {
			fmt.Printf("  %s Set remote URL failed, rolling back...\n", ui.WarningIcon())
			if failures := state.rollback(ctx); len(failures) > 0 {
				printRecoveryInstructions(failures)
			} else {
				fmt.Printf("  %s Rollback succeeded — no changes were applied\n", ui.SuccessIcon())
			}
		}
		return fmt.Errorf("set remote URL in project: %w", err)
	}
	state.remoteURLUpdated = true
	state.addStep(fmt.Sprintf("set remote %s URL", remoteName))
	fmt.Printf("  %s Set remote %s URL\n", ui.SuccessIcon(), ui.Value(remoteName))

	// Step 4: Verify connectivity (non-fatal)
	if !noVerify {
		if err := git.VerifyRemote(ctx, resolved.Path, remoteName); err != nil {
			fmt.Printf("  %s Connectivity check failed: %s\n", ui.WarningIcon(), ui.Dim(err.Error()))
			fmt.Printf("  %s URL was updated but remote may not be reachable\n", ui.WarningIcon())
		} else {
			fmt.Printf("  %s Remote verified reachable\n", ui.SuccessIcon())
		}
	}

	// Step 5: Auto-stage .gitmodules (non-fatal)
	if isSubmodule && !noStage {
		stageErr := git.WithLockRetry(ctx, campRoot, git.DefaultRetryConfig(), func() error {
			return git.StageFiles(ctx, campRoot, ".gitmodules")
		})
		if stageErr != nil {
			fmt.Printf("  %s Could not stage .gitmodules: %s\n", ui.WarningIcon(), ui.Dim(stageErr.Error()))
		} else {
			state.addStep("staged .gitmodules")
			fmt.Printf("  %s Staged .gitmodules\n", ui.SuccessIcon())
		}
	}

	// Show AFTER state
	fmt.Println()
	fmt.Printf("%s Remote URL updated:\n", ui.SuccessIcon())
	fmt.Printf("  %s\n", ui.Value(newURL))

	return nil
}

func printRecoveryInstructions(instructions []string) {
	fmt.Printf("\n%s Automatic rollback failed. Manual recovery required:\n\n", ui.WarningIcon())
	for _, line := range instructions {
		fmt.Println(line)
	}
	fmt.Println()
}
