// Package ui provides styled terminal output for the camp CLI.
package ui

import "github.com/charmbracelet/lipgloss"

// Status colors for campaign states
var (
	SuccessColor  = lipgloss.Color("42")  // Green
	InfoColor     = lipgloss.Color("33")  // Blue
	WarningColor  = lipgloss.Color("220") // Yellow/amber
	ErrorColor    = lipgloss.Color("196") // Red
	DimColor      = lipgloss.Color("245") // Grey
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
