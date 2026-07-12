//go:build dev

package quest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/ledger"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/ui"
)

type questCommandContext struct {
	cfg          *config.CampaignConfig
	campaignRoot string
	service      *quest.Service
}

func loadQuestCommandContext(ctx context.Context, ensureScaffold bool) (*questCommandContext, error) {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "not in a campaign directory")
	}
	campaignRoot, err = pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolving campaign root")
	}

	if ensureScaffold {
		if _, err := quest.EnsureScaffold(ctx, campaignRoot); err != nil {
			return nil, camperrors.Wrap(err, "ensuring quest scaffold")
		}
	}

	svc := quest.NewService(campaignRoot)
	svc.SetLedger(ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnToStderr()))
	return &questCommandContext{
		cfg:          cfg,
		campaignRoot: campaignRoot,
		service:      svc,
	}, nil
}

func parseQuestStatuses(values []string) ([]quest.Status, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]quest.Status, 0, len(values))
	for _, value := range values {
		status, err := quest.ParseStatus(value)
		if err != nil {
			return nil, err
		}
		result = append(result, status)
	}
	return result, nil
}

func parseQuestTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var tags []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func completeQuestSelector(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	quests, err := qctx.service.List(ctx, &quest.ListOptions{All: true})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	seen := map[string]struct{}{}
	prefix := strings.ToLower(toComplete)
	var matches []string
	for _, q := range quests {
		for _, candidate := range []string{q.ID, q.Slug, q.Name} {
			if candidate == "" || !strings.HasPrefix(strings.ToLower(candidate), prefix) {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			matches = append(matches, candidate)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

type questListJSONPayload struct {
	SchemaVersion string         `json:"schema_version"`
	CampaignRoot  string         `json:"campaign_root"`
	Items         []*quest.Quest `json:"items"`
}

type questShowJSONPayload struct {
	SchemaVersion string       `json:"schema_version"`
	CampaignRoot  string       `json:"campaign_root"`
	Quest         *quest.Quest `json:"quest"`
}

func outputQuestListJSON(qctx *questCommandContext, quests []*quest.Quest) error {
	items, err := questsForJSON(qctx, quests)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(questListJSONPayload{
		SchemaVersion: QuestListJSONVersion,
		CampaignRoot:  qctx.campaignRoot,
		Items:         items,
	})
}

func outputQuestShowJSON(qctx *questCommandContext, q *quest.Quest) error {
	item, err := questForJSON(qctx, q)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(questShowJSONPayload{
		SchemaVersion: QuestShowJSONVersion,
		CampaignRoot:  qctx.campaignRoot,
		Quest:         item,
	})
}

func questsForJSON(qctx *questCommandContext, quests []*quest.Quest) ([]*quest.Quest, error) {
	items := make([]*quest.Quest, 0, len(quests))
	for _, q := range quests {
		item, err := questForJSON(qctx, q)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func questForJSON(qctx *questCommandContext, q *quest.Quest) (*quest.Quest, error) {
	item := q.Clone()
	if item == nil {
		return nil, nil
	}
	relPath, err := pathutil.RelativeToRoot(qctx.campaignRoot, item.Path)
	if err != nil {
		return nil, camperrors.Wrap(err, "relativizing quest path")
	}
	item.Path = relPath
	return item, nil
}

func outputQuestTable(qctx *questCommandContext, quests []*quest.Quest) error {
	if len(quests) == 0 {
		fmt.Println("No quests found.")
		return nil
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	statusStyle := func(s quest.Status) string {
		return ui.GetQuestStatusStyle(string(s)).Render(string(s))
	}

	headers := []string{"NAME", "STATUS", "ID", "UPDATED", "PATH"}
	rows := make([][]string, 0, len(quests))

	for _, q := range quests {
		updated := q.UpdatedAt
		if updated.IsZero() {
			updated = q.CreatedAt
		}
		rows = append(rows, []string{
			q.Name,
			statusStyle(q.Status),
			q.ID,
			updated.Format("2006-01-02 15:04"),
			quest.RelativePath(qctx.campaignRoot, q.Path),
		})
	}

	t := table.New().
		Border(lipgloss.HiddenBorder()).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return lipgloss.NewStyle()
		})

	fmt.Println(t)
	fmt.Printf("\n%d quest(s)\n", len(quests))
	return nil
}

func outputQuestShow(qctx *questCommandContext, q *quest.Quest) {
	fmt.Printf("Name:        %s\n", q.Name)
	fmt.Printf("ID:          %s\n", q.ID)
	fmt.Printf("Status:      %s\n", q.Status)
	if q.Purpose != "" {
		fmt.Printf("Purpose:     %s\n", q.Purpose)
	}
	fmt.Printf("Created:     %s\n", q.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:     %s\n", q.UpdatedAt.Format(time.RFC3339))
	if len(q.Tags) > 0 {
		fmt.Printf("Tags:        %s\n", strings.Join(q.Tags, ", "))
	}
	if q.Path != "" {
		fmt.Printf("Path:        %s\n", quest.RelativePath(qctx.campaignRoot, q.Path))
	}
	if len(q.Links) > 0 {
		fmt.Printf("Links:\n")
		for _, link := range q.Links {
			fmt.Printf("  %-10s %s\n", link.Type, link.Path)
		}
	}
	if q.Description != "" {
		fmt.Printf("\n%s\n", q.Description)
	}
}

func autoCommitQuest(ctx context.Context, qctx *questCommandContext, action commit.QuestAction, result *quest.MutationResult, detail string) error {
	if result == nil || result.Quest == nil {
		return nil
	}
	files := commit.NormalizeFiles(qctx.campaignRoot, result.Files...)
	preStaged := commit.NormalizeFiles(qctx.campaignRoot, result.PreStaged...)

	// PreStaged paths are old directories that were renamed away by os.Rename.
	// git does not detect directory moves automatically, so the deletion side
	// must be explicitly removed from the index before committing.
	if len(preStaged) > 0 {
		if err := git.RemoveCached(ctx, qctx.campaignRoot, preStaged...); err != nil {
			return camperrors.Wrap(err, "stage deletion of old quest path for auto-commit")
		}
	}

	commitResult := commit.Quest(ctx, commit.QuestOptions{
		Options: commit.Options{
			CampaignRoot:  qctx.campaignRoot,
			CampaignID:    qctx.cfg.ID,
			QuestID:       result.Quest.ID,
			Files:         files,
			PreStaged:     preStaged,
			SelectiveOnly: len(files) > 0 || len(preStaged) > 0,
		},
		Action:    action,
		QuestID:   result.Quest.ID,
		QuestName: result.Quest.Name,
		Detail:    detail,
	})
	if commitResult.Err != nil {
		return camperrors.Wrap(commitResult.Err, "auto-commit quest changes")
	}
	if commitResult.Message != "" {
		fmt.Printf("  %s\n", commitResult.Message)
	}
	return nil
}
