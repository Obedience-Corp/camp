//go:build dev

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/quest"
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

	if ensureScaffold {
		if _, err := quest.EnsureScaffold(ctx, campaignRoot); err != nil {
			return nil, camperrors.Wrap(err, "ensuring quest scaffold")
		}
	}

	return &questCommandContext{
		cfg:          cfg,
		campaignRoot: campaignRoot,
		service:      quest.NewService(campaignRoot),
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

func outputQuestJSON(quests []*quest.Quest) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(quests)
}

func outputQuestTable(qctx *questCommandContext, quests []*quest.Quest) error {
	if len(quests) == 0 {
		fmt.Println("No quests found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tID\tUPDATED\tPATH")
	for _, q := range quests {
		updated := q.UpdatedAt
		if updated.IsZero() {
			updated = q.CreatedAt
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			q.Name,
			q.Status,
			q.ID,
			updated.Format("2006-01-02 15:04"),
			quest.RelativePath(qctx.campaignRoot, q.Path),
		)
	}
	_ = w.Flush()
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
	if q.Description != "" {
		fmt.Printf("\n%s\n", q.Description)
	}
}

func autoCommitQuest(ctx context.Context, qctx *questCommandContext, action commit.QuestAction, result *quest.MutationResult, detail string) {
	if result == nil || result.Quest == nil {
		return
	}
	files := commit.NormalizeFiles(qctx.campaignRoot, result.Files...)
	preStaged := commit.NormalizeFiles(qctx.campaignRoot, result.PreStaged...)
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
	if commitResult.Message != "" {
		fmt.Printf("  %s\n", commitResult.Message)
	}
}
