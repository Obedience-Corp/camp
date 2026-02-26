package clone

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/sync"
)

func TestNewCloner(t *testing.T) {
	c := NewCloner()
	if c == nil {
		t.Fatal("NewCloner() returned nil")
	}
}

func TestNewClonerWithOptions(t *testing.T) {
	c := NewCloner(
		WithURL("https://github.com/test/repo.git"),
		WithDirectory("my-dir"),
		WithBranch("main"),
		WithDepth(1),
		WithNoSubmodules(true),
		WithNoValidate(true),
		WithVerbose(true),
		WithJSON(true),
	)

	opts := c.Options()

	if opts.URL != "https://github.com/test/repo.git" {
		t.Errorf("URL = %q, want %q", opts.URL, "https://github.com/test/repo.git")
	}
	if opts.Directory != "my-dir" {
		t.Errorf("Directory = %q, want %q", opts.Directory, "my-dir")
	}
	if opts.Branch != "main" {
		t.Errorf("Branch = %q, want %q", opts.Branch, "main")
	}
	if opts.Depth != 1 {
		t.Errorf("Depth = %d, want %d", opts.Depth, 1)
	}
	if !opts.NoSubmodules {
		t.Error("NoSubmodules = false, want true")
	}
	if !opts.NoValidate {
		t.Error("NoValidate = false, want true")
	}
	if !opts.Verbose {
		t.Error("Verbose = false, want true")
	}
	if !opts.JSON {
		t.Error("JSON = false, want true")
	}
}

func TestWithURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"https URL", "https://github.com/org/repo.git"},
		{"ssh URL", "git@github.com:org/repo.git"},
		{"ssh:// URL", "ssh://git@github.com/org/repo.git"},
		{"empty URL", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCloner(WithURL(tt.url))
			if c.options.URL != tt.url {
				t.Errorf("URL = %q, want %q", c.options.URL, tt.url)
			}
		})
	}
}

func TestWithDirectory(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{"simple name", "my-repo"},
		{"with path", "projects/my-repo"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCloner(WithDirectory(tt.dir))
			if c.options.Directory != tt.dir {
				t.Errorf("Directory = %q, want %q", c.options.Directory, tt.dir)
			}
		})
	}
}

func TestWithBranch(t *testing.T) {
	tests := []struct {
		name   string
		branch string
	}{
		{"main branch", "main"},
		{"feature branch", "feature/new-feature"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCloner(WithBranch(tt.branch))
			if c.options.Branch != tt.branch {
				t.Errorf("Branch = %q, want %q", c.options.Branch, tt.branch)
			}
		})
	}
}

func TestWithDepth(t *testing.T) {
	tests := []struct {
		name     string
		depth    int
		expected int
	}{
		{"positive depth", 5, 5},
		{"depth 1 (shallow)", 1, 1},
		{"zero (full)", 0, 0},
		{"negative (ignored)", -1, 0}, // Negative depths should be ignored
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCloner(WithDepth(tt.depth))
			if c.options.Depth != tt.expected {
				t.Errorf("Depth = %d, want %d", c.options.Depth, tt.expected)
			}
		})
	}
}

func TestWithNoSubmodules(t *testing.T) {
	tests := []struct {
		name         string
		noSubmodules bool
	}{
		{"enabled", true},
		{"disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCloner(WithNoSubmodules(tt.noSubmodules))
			if c.options.NoSubmodules != tt.noSubmodules {
				t.Errorf("NoSubmodules = %v, want %v", c.options.NoSubmodules, tt.noSubmodules)
			}
		})
	}
}

func TestWithNoValidate(t *testing.T) {
	tests := []struct {
		name       string
		noValidate bool
	}{
		{"enabled", true},
		{"disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCloner(WithNoValidate(tt.noValidate))
			if c.options.NoValidate != tt.noValidate {
				t.Errorf("NoValidate = %v, want %v", c.options.NoValidate, tt.noValidate)
			}
		})
	}
}

func TestWithVerbose(t *testing.T) {
	tests := []struct {
		name    string
		verbose bool
	}{
		{"enabled", true},
		{"disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCloner(WithVerbose(tt.verbose))
			if c.options.Verbose != tt.verbose {
				t.Errorf("Verbose = %v, want %v", c.options.Verbose, tt.verbose)
			}
		})
	}
}

func TestWithJSON(t *testing.T) {
	tests := []struct {
		name string
		json bool
	}{
		{"enabled", true},
		{"disabled", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCloner(WithJSON(tt.json))
			if c.options.JSON != tt.json {
				t.Errorf("JSON = %v, want %v", c.options.JSON, tt.json)
			}
		})
	}
}

func TestWithSyncer(t *testing.T) {
	// Create a syncer instance
	syncer := sync.NewSyncer("/tmp/test")

	c := NewCloner(WithSyncer(syncer))
	if c.syncer != syncer {
		t.Error("WithSyncer() did not set syncer correctly")
	}
}

func TestWithSyncer_Nil(t *testing.T) {
	c := NewCloner(WithSyncer(nil))
	if c.syncer != nil {
		t.Error("WithSyncer(nil) should set syncer to nil")
	}
}

func TestOptions_ReturnsCurrentOptions(t *testing.T) {
	c := NewCloner(
		WithURL("https://github.com/test/repo.git"),
		WithBranch("develop"),
	)

	opts := c.Options()

	// Verify options are returned correctly
	if opts.URL != "https://github.com/test/repo.git" {
		t.Errorf("Options().URL = %q, want %q", opts.URL, "https://github.com/test/repo.git")
	}
	if opts.Branch != "develop" {
		t.Errorf("Options().Branch = %q, want %q", opts.Branch, "develop")
	}
}

func TestExitCodes(t *testing.T) {
	// Verify exit code constants are defined correctly
	tests := []struct {
		name     string
		code     int
		expected int
	}{
		{"ExitSuccess", ExitSuccess, 0},
		{"ExitCloneFailed", ExitCloneFailed, 1},
		{"ExitPartialSuccess", ExitPartialSuccess, 2},
		{"ExitValidationFailed", ExitValidationFailed, 3},
		{"ExitInvalidArgs", ExitInvalidArgs, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.expected)
			}
		})
	}
}
