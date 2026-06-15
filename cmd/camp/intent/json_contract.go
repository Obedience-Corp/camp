package intent

import (
	"encoding/json"
	"io"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/spf13/cobra"
)

const IntentJSONVersion = "intents/v1alpha1"

// IntentPayload is the top-level JSON output for intent commands --json.
type IntentPayload struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	CampaignRoot  string       `json:"campaign_root"`
	Items         []IntentItem `json:"items"`
}

// IntentItem is one intent in the JSON output.
type IntentItem struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Type              string   `json:"type"`
	Status            string   `json:"status"`
	Concept           string   `json:"concept,omitempty"`
	Author            string   `json:"author,omitempty"`
	Priority          string   `json:"priority,omitempty"`
	Horizon           string   `json:"horizon,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	BlockedBy         []string `json:"blocked_by,omitempty"`
	DependsOn         []string `json:"depends_on,omitempty"`
	PromotionCriteria string   `json:"promotion_criteria,omitempty"`
	PromotedTo        string   `json:"promoted_to,omitempty"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at,omitempty"`
	Path              string   `json:"path"`
}

type IntentCountPayload struct {
	SchemaVersion string            `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	CampaignRoot  string            `json:"campaign_root"`
	Items         []IntentCountItem `json:"items"`
	Total         int               `json:"total"`
}

type IntentCountItem struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type IntentAddPayload struct {
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	CampaignRoot  string    `json:"campaign_root"`
	ID            string    `json:"id"`
	Path          string    `json:"path"`
}

func outputIntentPayload(w io.Writer, campaignRoot string, intents []*intentcore.Intent) error {
	items := make([]IntentItem, 0, len(intents))
	for _, i := range intents {
		item, err := intentItemFromIntent(campaignRoot, i)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	return encodeIntentJSON(w, IntentPayload{
		SchemaVersion: IntentJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		CampaignRoot:  campaignRoot,
		Items:         items,
	})
}

func outputIntentCountPayload(w io.Writer, campaignRoot string, counts []intentcore.StatusCount, total int) error {
	items := make([]IntentCountItem, 0, len(counts))
	for _, sc := range counts {
		items = append(items, IntentCountItem{
			Status: string(sc.Status),
			Count:  sc.Count,
		})
	}
	return encodeIntentJSON(w, IntentCountPayload{
		SchemaVersion: IntentJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		CampaignRoot:  campaignRoot,
		Items:         items,
		Total:         total,
	})
}

func outputIntentAddPayload(w io.Writer, campaignRoot string, i *intentcore.Intent) error {
	relPath, err := pathutil.RelativeToRoot(campaignRoot, i.Path)
	if err != nil {
		return camperrors.Wrap(err, "relativizing intent path")
	}
	return encodeIntentJSON(w, IntentAddPayload{
		SchemaVersion: IntentJSONVersion,
		GeneratedAt:   time.Now().UTC(),
		CampaignRoot:  campaignRoot,
		ID:            i.ID,
		Path:          relPath,
	})
}

func intentItemFromIntent(campaignRoot string, i *intentcore.Intent) (IntentItem, error) {
	relPath, err := pathutil.RelativeToRoot(campaignRoot, i.Path)
	if err != nil {
		return IntentItem{}, camperrors.Wrap(err, "relativizing intent path")
	}
	item := IntentItem{
		ID:                i.ID,
		Title:             i.Title,
		Type:              string(i.Type),
		Status:            string(i.Status),
		Concept:           i.Concept,
		Author:            i.Author,
		Priority:          string(i.Priority),
		Horizon:           string(i.Horizon),
		Tags:              i.Tags,
		BlockedBy:         i.BlockedBy,
		DependsOn:         i.DependsOn,
		PromotionCriteria: i.PromotionCriteria,
		PromotedTo:        i.PromotedTo,
		CreatedAt:         i.CreatedAt.Format(time.RFC3339),
		Path:              relPath,
	}
	if !i.UpdatedAt.IsZero() {
		item.UpdatedAt = i.UpdatedAt.Format(time.RFC3339)
	}
	return item, nil
}

func encodeIntentJSON(w io.Writer, payload any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return camperrors.Wrap(err, "failed to marshal JSON")
	}
	return nil
}

func intentJSONRequested(cmd *cobra.Command, jsonOut *bool) bool {
	if jsonOut != nil && *jsonOut {
		return true
	}
	format, err := cmd.Flags().GetString("format")
	return err == nil && format == "json"
}
