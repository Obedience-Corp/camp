package scaffold

import "embed"

// CampaignScaffoldFS contains the embedded scaffold definitions and templates.
//
//go:embed campaign/scaffold.yaml campaign/scaffold-minimal.yaml campaign/templates/*
var CampaignScaffoldFS embed.FS
