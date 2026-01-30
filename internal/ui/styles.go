// Package ui provides styled terminal output for the camp CLI.
package ui

import "github.com/charmbracelet/lipgloss"

// Status colors for campaign states
var (
	SuccessColor  = lipgloss.Color("42")  // Green
	InfoColor     = lipgloss.Color("33")  // Blue
	WarningColor  = lipgloss.Color("220") // Yellow/amber
	ErrorColor    = lipgloss.Color("196") // Red
	DimColor      = lipgloss.Color("253") // Very light grey (readable on dark backgrounds)
	BrightColor   = lipgloss.Color("255") // White
	AccentColor   = lipgloss.Color("51")  // Cyan
	CategoryColor = lipgloss.Color("141") // Purple
)

// Campaign type colors
var (
	ProductColor  = lipgloss.Color("42")  // Green
	ResearchColor = lipgloss.Color("33")  // Blue
	ToolsColor    = lipgloss.Color("220") // Yellow
	PersonalColor = lipgloss.Color("141") // Purple
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

// Intent type colors
var (
	TypeFeatureColor  = lipgloss.Color("42")  // Green - new functionality
	TypeBugColor      = lipgloss.Color("196") // Red - something broken
	TypeIdeaColor     = lipgloss.Color("141") // Purple - creative/exploratory
	TypeResearchColor = lipgloss.Color("33")  // Blue - investigation
	TypeChoreColor    = lipgloss.Color("245") // Grey - maintenance
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
