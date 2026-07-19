// Package ui provides styled terminal output for the camp CLI.
package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var sharedPalette = theme.CurrentPalette()

func sharedColor(value string) lipgloss.Color {
	if value == "" {
		// Plain mode uses Lip Gloss's Ascii profile; keep style colors
		// non-nil for components that render whitespace backgrounds.
		return lipgloss.Color("0")
	}
	return lipgloss.Color(value)
}

// Status colors use the semantic roles from obey-shared/brand.
var (
	SuccessColor  = sharedColor(sharedPalette.StatusSuccess)
	InfoColor     = sharedColor(sharedPalette.AccentHighlight)
	WarningColor  = sharedColor(sharedPalette.StatusWarning)
	ErrorColor    = sharedColor(sharedPalette.StatusError)
	DimColor      = sharedColor(sharedPalette.TextMuted)
	BrightColor   = sharedColor(sharedPalette.TextPrimary)
	AccentColor   = sharedColor(sharedPalette.Accent)
	CategoryColor = sharedColor(sharedPalette.AccentSubtle)
)

// Campaign type colors intentionally compose the shared semantic roles; the
// category distinction is a product concern, not a second palette.
var (
	ProductColor  = AccentColor
	ResearchColor = InfoColor
	ToolsColor    = WarningColor
	PersonalColor = CategoryColor
)

// Intent status colors - reuse existing semantic colors for consistency
var (
	StatusInboxColor  = DimColor     // Light grey for inbox
	StatusActiveColor = SuccessColor // Green for active
	StatusReadyColor  = WarningColor // Yellow for ready
	StatusDoneColor   = SuccessColor // Green for done
	StatusKilledColor = ErrorColor   // Red for killed
)

// Pre-built styles
var (
	successStyle  = lipgloss.NewStyle().Foreground(SuccessColor).Bold(true)
	errorStyle    = lipgloss.NewStyle().Foreground(ErrorColor).Bold(true)
	warningStyle  = lipgloss.NewStyle().Foreground(WarningColor).Bold(true)
	infoStyle     = lipgloss.NewStyle().Foreground(InfoColor).Bold(true)
	labelStyle    = lipgloss.NewStyle().Foreground(DimColor).Bold(true)
	valueStyle    = lipgloss.NewStyle().Foreground(BrightColor).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(DimColor)
	accentStyle   = lipgloss.NewStyle().Foreground(AccentColor).Bold(true)
	categoryStyle = lipgloss.NewStyle().Foreground(CategoryColor).Bold(true)
	headerStyle   = lipgloss.NewStyle().Foreground(BrightColor).Bold(true).Underline(true)
	subheadStyle  = lipgloss.NewStyle().Foreground(BrightColor).Bold(true)
)

// GetCampaignTypeStyle returns the style for a campaign type
func GetCampaignTypeStyle(campaignType string) lipgloss.Style {
	switch campaignType {
	case "product":
		return lipgloss.NewStyle().Foreground(ProductColor).Bold(true)
	case "research":
		return lipgloss.NewStyle().Foreground(ResearchColor).Bold(true)
	case "tools":
		return lipgloss.NewStyle().Foreground(ToolsColor).Bold(true)
	case "personal":
		return lipgloss.NewStyle().Foreground(PersonalColor).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(DimColor)
	}
}

// GetCampaignTypeColor returns the color for a campaign type
func GetCampaignTypeColor(campaignType string) lipgloss.Color {
	switch campaignType {
	case "product":
		return ProductColor
	case "research":
		return ResearchColor
	case "tools":
		return ToolsColor
	case "personal":
		return PersonalColor
	default:
		return DimColor
	}
}

// GetIntentStatusStyle returns the style for an intent status
func GetIntentStatusStyle(status string) lipgloss.Style {
	switch status {
	case "inbox":
		return lipgloss.NewStyle().Foreground(StatusInboxColor)
	case "active":
		return lipgloss.NewStyle().Foreground(StatusActiveColor)
	case "ready":
		return lipgloss.NewStyle().Foreground(StatusReadyColor)
	case "done":
		return lipgloss.NewStyle().Foreground(StatusDoneColor)
	case "killed":
		return lipgloss.NewStyle().Foreground(StatusKilledColor)
	default:
		return lipgloss.NewStyle().Foreground(DimColor)
	}
}

// Intent type colors intentionally compose the shared semantic roles.
var (
	TypeFeatureColor  = AccentColor
	TypeBugColor      = ErrorColor
	TypeIdeaColor     = CategoryColor
	TypeResearchColor = InfoColor
	TypeChoreColor    = DimColor
)

// GetIntentTypeStyle returns the style for an intent type
func GetIntentTypeStyle(intentType string) lipgloss.Style {
	switch intentType {
	case "feature":
		return lipgloss.NewStyle().Foreground(TypeFeatureColor)
	case "bug":
		return lipgloss.NewStyle().Foreground(TypeBugColor)
	case "idea":
		return lipgloss.NewStyle().Foreground(TypeIdeaColor)
	case "research":
		return lipgloss.NewStyle().Foreground(TypeResearchColor)
	case "chore":
		return lipgloss.NewStyle().Foreground(TypeChoreColor)
	default:
		return lipgloss.NewStyle().Foreground(DimColor)
	}
}

// GetConceptStyle returns the style for a concept/project name
func GetConceptStyle(concept string) lipgloss.Style {
	if concept == "" || concept == "-" {
		return lipgloss.NewStyle().Foreground(DimColor)
	}
	return lipgloss.NewStyle().Foreground(AccentColor)
}

// Workflow type colors intentionally compose the shared semantic roles.
var (
	WorkflowIntentColor   = AccentColor
	WorkflowDesignColor   = InfoColor
	WorkflowExploreColor  = WarningColor
	WorkflowFestivalColor = SuccessColor
)

// GetWorkflowTypeStyle returns the style for a workflow type (intent, design, explore, festival).
func GetWorkflowTypeStyle(wfType string) lipgloss.Style {
	switch wfType {
	case "intent":
		return lipgloss.NewStyle().Foreground(WorkflowIntentColor)
	case "design":
		return lipgloss.NewStyle().Foreground(WorkflowDesignColor)
	case "explore":
		return lipgloss.NewStyle().Foreground(WorkflowExploreColor)
	case "festival":
		return lipgloss.NewStyle().Foreground(WorkflowFestivalColor)
	default:
		return lipgloss.NewStyle().Foreground(DimColor)
	}
}

// GetWorkflowTypeColor returns the color for a workflow type.
func GetWorkflowTypeColor(wfType string) lipgloss.TerminalColor {
	switch wfType {
	case "intent":
		return WorkflowIntentColor
	case "design":
		return WorkflowDesignColor
	case "explore":
		return WorkflowExploreColor
	case "festival":
		return WorkflowFestivalColor
	default:
		return DimColor
	}
}

// GetQuestStatusStyle returns the style for a quest status.
func GetQuestStatusStyle(status string) lipgloss.Style {
	switch status {
	case "open":
		return lipgloss.NewStyle().Foreground(StatusActiveColor)
	case "paused":
		return lipgloss.NewStyle().Foreground(WarningColor)
	case "completed":
		return lipgloss.NewStyle().Foreground(SuccessColor)
	case "archived":
		return lipgloss.NewStyle().Foreground(DimColor)
	default:
		return lipgloss.NewStyle().Foreground(DimColor)
	}
}

// GetLifecycleStageStyle returns the style for a lifecycle stage.
func GetLifecycleStageStyle(stage string) lipgloss.Style {
	switch stage {
	case "inbox":
		return lipgloss.NewStyle().Foreground(StatusInboxColor)
	case "active":
		return lipgloss.NewStyle().Foreground(StatusActiveColor)
	case "ready":
		return lipgloss.NewStyle().Foreground(StatusReadyColor)
	case "planning":
		return lipgloss.NewStyle().Foreground(InfoColor)
	case "done":
		return lipgloss.NewStyle().Foreground(StatusDoneColor)
	case "killed":
		return lipgloss.NewStyle().Foreground(StatusKilledColor)
	default:
		return lipgloss.NewStyle().Foreground(DimColor)
	}
}
