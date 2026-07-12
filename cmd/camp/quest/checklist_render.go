//go:build dev

package quest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// QuestChecklistJSONVersion is the schema tag for checklist JSON output. It
// matches the on-disk checklist schema so agents can key off a single version.
const QuestChecklistJSONVersion = quest.ChecklistSchemaV1

type checklistQuestJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type checklistWorkitemJSON struct {
	ID             string `json:"id"`
	Ref            string `json:"ref,omitempty"`
	ResolvedPath   string `json:"resolved_path,omitempty"`
	AttentionStage string `json:"attention_stage,omitempty"`
	LifecycleStage string `json:"lifecycle_stage,omitempty"`
	Missing        bool   `json:"missing,omitempty"`
}

type checklistItemJSON struct {
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Status      string                 `json:"status"`
	Rank        int                    `json:"rank"`
	Workitem    *checklistWorkitemJSON `json:"workitem,omitempty"`
	Notes       string                 `json:"notes,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

type checklistJSON struct {
	SchemaVersion string              `json:"schema_version"`
	CampaignRoot  string              `json:"campaign_root"`
	Quest         checklistQuestJSON  `json:"quest"`
	Items         []checklistItemJSON `json:"items"`
}

type checklistItemResultJSON struct {
	SchemaVersion string             `json:"schema_version"`
	CampaignRoot  string             `json:"campaign_root"`
	Quest         checklistQuestJSON `json:"quest"`
	Item          checklistItemJSON  `json:"item"`
}

func questJSON(q *quest.Quest) checklistQuestJSON {
	return checklistQuestJSON{ID: q.ID, Name: q.Name, Status: string(q.Status)}
}

type checklistWorkitemIndex map[string]workitem.WorkItem

// loadChecklistWorkitemIndex discovers once per render. Checklist links store
// stable workitem ids, so the read-time join does not need to repeat a full
// campaign discovery for every row.
func loadChecklistWorkitemIndex(ctx context.Context, root string) (checklistWorkitemIndex, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("load campaign config for checklist workitems: %w", err)
	}
	items, err := workitem.Discover(ctx, root, paths.NewResolverFromConfig(root, cfg))
	if err != nil {
		return nil, fmt.Errorf("discover checklist workitems: %w", err)
	}
	index := make(checklistWorkitemIndex, len(items))
	for _, item := range items {
		if item.StableID != "" {
			index[item.StableID] = item
		}
	}
	return index, nil
}

// resolveChecklistWorkitem performs the read-time join: the stored id is the
// source of truth, and the path/stage are looked up from the current discovery.
// Workitems outside the discoverable inventory (including dungeoned or deleted
// items) surface as missing rather than retaining a stale path.
func resolveChecklistWorkitem(index checklistWorkitemIndex, wi *quest.ChecklistWorkitem) *checklistWorkitemJSON {
	if wi == nil {
		return nil
	}
	out := &checklistWorkitemJSON{ID: wi.ID, Ref: wi.Ref}
	w, ok := index[wi.ID]
	if !ok {
		out.Missing = true
		return out
	}
	out.ResolvedPath = w.RelativePath
	out.AttentionStage = w.AttentionStage
	out.LifecycleStage = string(w.LifecycleStage)
	if out.Ref == "" && w.StableID != "" {
		out.Ref = workitem.Derive(w.StableID)
	}
	return out
}

func checklistItemToJSON(index checklistWorkitemIndex, item quest.ChecklistItem) checklistItemJSON {
	return checklistItemJSON{
		ID:          item.ID,
		Title:       item.Title,
		Status:      string(item.Status),
		Rank:        item.Rank,
		Workitem:    resolveChecklistWorkitem(index, item.Workitem),
		Notes:       item.Notes,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
		CompletedAt: item.CompletedAt,
	}
}

func outputChecklistJSON(ctx context.Context, w io.Writer, root string, q *quest.Quest, items []quest.ChecklistItem) error {
	index, err := loadChecklistWorkitemIndex(ctx, root)
	if err != nil {
		return err
	}
	jsonItems := make([]checklistItemJSON, 0, len(items))
	for _, item := range items {
		jsonItems = append(jsonItems, checklistItemToJSON(index, item))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(checklistJSON{
		SchemaVersion: QuestChecklistJSONVersion,
		CampaignRoot:  root,
		Quest:         questJSON(q),
		Items:         jsonItems,
	})
}

func outputChecklistItemResultJSON(ctx context.Context, w io.Writer, root string, q *quest.Quest, item *quest.ChecklistItem) error {
	index, err := loadChecklistWorkitemIndex(ctx, root)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(checklistItemResultJSON{
		SchemaVersion: QuestChecklistJSONVersion,
		CampaignRoot:  root,
		Quest:         questJSON(q),
		Item:          checklistItemToJSON(index, *item),
	})
}

// statusMark renders a compact checkbox glyph for an item status.
func statusMark(status quest.ChecklistItemStatus) string {
	switch status {
	case quest.ItemDone:
		return "[x]"
	case quest.ItemDoing:
		return "[~]"
	case quest.ItemDropped:
		return "[-]"
	default:
		return "[ ]"
	}
}

// shortItemID returns the trailing suffix of a checklist item id for display.
// The full id remains a valid selector; the suffix is enough to disambiguate.
func shortItemID(id string) string {
	if idx := strings.LastIndex(id, "_"); idx >= 0 && idx+1 < len(id) {
		return id[idx+1:]
	}
	return id
}

func workitemCell(index checklistWorkitemIndex, wi *quest.ChecklistWorkitem) string {
	if wi == nil {
		return ""
	}
	resolved := resolveChecklistWorkitem(index, wi)
	if resolved.Missing {
		label := resolved.Ref
		if label == "" {
			label = resolved.ID
		}
		return label + " (missing)"
	}
	if resolved.ResolvedPath != "" {
		return resolved.ResolvedPath
	}
	if resolved.Ref != "" {
		return resolved.Ref
	}
	return resolved.ID
}

func outputChecklistTable(ctx context.Context, w io.Writer, root string, q *quest.Quest, items []quest.ChecklistItem) error {
	index, err := loadChecklistWorkitemIndex(ctx, root)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Quest: %s (%s)\n", q.Name, q.ID)
	if len(items) == 0 {
		fmt.Fprintln(w, "No checklist items. Add one with: camp quest item add "+q.Name+" \"<title>\"")
		return nil
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)
	headers := []string{"", "ITEM", "RANK", "TITLE", "WORKITEM"}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			statusMark(item.Status),
			shortItemID(item.ID),
			fmt.Sprintf("%d", item.Rank),
			item.Title,
			workitemCell(index, item.Workitem),
		})
	}

	t := table.New().
		Border(lipgloss.HiddenBorder()).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return lipgloss.NewStyle()
		})

	fmt.Fprintln(w, t)
	open := 0
	for _, item := range items {
		if !item.Status.Terminal() {
			open++
		}
	}
	fmt.Fprintf(w, "\n%d item(s), %d open\n", len(items), open)
	return nil
}
