//go:build dev

package quest

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/quest"
)

var questLinkCmd = &cobra.Command{
	Use:   "link <quest> <path>",
	Short: "Link a campaign artifact to a quest",
	Long: `Associate a campaign artifact (intent, design, festival, project, document)
with a quest for traceability.

The link type is auto-detected from the path:
  workflow/intents/  → intent
  workflow/design/   → design
  workflow/explore/  → explore
  festivals/         → festival
  projects/          → project
  (other)            → document

Use --type to override auto-detection.

Examples:
  camp quest link myquest workflow/intents/some-intent.yaml
  camp quest link myquest projects/camp
  camp quest link myquest docs/spec.md --type spec`,
	Args: cobra.ExactArgs(2),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest link operation",
	},
	RunE: runQuestLink,
}

func init() {
	Cmd.AddCommand(questLinkCmd)

	questLinkCmd.Flags().String("type", "", "Override auto-detected link type")
	questLinkCmd.Flags().Bool("no-commit", false, "Don't create a git commit")
	questLinkCmd.ValidArgsFunction = completeQuestSelector
}

func runQuestLink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	selector := args[0]
	rawPath := args[1]
	linkType, _ := cmd.Flags().GetString("type")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	// Normalize path to be relative to campaign root
	path, err := normalizeLinkPath(qctx.campaignRoot, rawPath)
	if err != nil {
		return err
	}

	result, err := qctx.service.Link(ctx, selector, path, linkType)
	if err != nil {
		return err
	}

	detectedType := linkType
	if detectedType == "" {
		detectedType = quest.DetectLinkType(path)
	}

	fmt.Printf("✓ Linked %s %s to quest %s\n", detectedType, path, result.Quest.Name)

	if !noCommit {
		if err := autoCommitQuest(ctx, qctx, commit.QuestLink, result, "Linked "+path); err != nil {
			return camperrors.Wrap(err, "quest linked, but auto-commit failed")
		}
	}
	return nil
}

// normalizeLinkPath converts an absolute or relative path to campaign-root-relative.
func normalizeLinkPath(campaignRoot, rawPath string) (string, error) {
	if filepath.IsAbs(rawPath) {
		rel, err := filepath.Rel(campaignRoot, rawPath)
		if err != nil {
			return "", camperrors.Wrapf(err, "path %s is not within campaign", rawPath)
		}
		return rel, nil
	}
	return rawPath, nil
}
