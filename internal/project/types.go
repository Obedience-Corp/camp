package project

// Project represents a project within a campaign.
type Project struct {
	// Name is the project directory name (e.g., "obey-platform-monorepo@obey").
	Name string
	// Path is the relative path from campaign root.
	Path string
	// Type is the detected project type (go, rust, typescript, etc.).
	Type string
	// URL is the git remote origin URL.
	URL string
	// MonorepoRoot is the relative path to the parent monorepo, set when this
	// project is a subproject expanded from a monorepo. Empty for standalone projects.
	MonorepoRoot string
}

// ProjectType constants for common project types.
const (
	TypeGo         = "go"
	TypeRust       = "rust"
	TypeTypeScript = "typescript"
	TypePython     = "python"
	TypeUnknown    = ""
)
