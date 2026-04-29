package shell

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestGenerateZsh(t *testing.T) {
	output := generateZsh()

	if output == "" {
		t.Fatal("generateZsh() returned empty string")
	}

	// Check for essential components
	checks := []struct {
		name    string
		content string
	}{
		{"camp wrapper function", "camp()"},
		{"cgo function", "cgo()"},
		{"cint function", "cint()"},
		{"cint implementation", "camp intent add"},
		{"cd command", "cd \"$dest\""},
		{"camp go call", "camp go"},
		{"command camp binary call", "command camp"},
		{"switch subcommand", "switch|sw)"},
		{"go subcommand", "go|g)"},
		{"workitem subcommand", "workitem|wi|workitems)"},
		{"workitem path output", "--path-output"},
		{"workitem root resolution", "command camp root"},
		{"workitem temp file", "mktemp"},
		{"workitem boolean passthrough", "--json|--json=*|--print|--print=*"},
		{"workitem tty guard", "[[ ! -t 0 || ! -t 1 ]]"},
		{"compdef cgo", "compdef _cgo cgo"},
		{"compdef camp", "compdef _camp camp"},
		{"has shortcut entries", "'p:"},
		{"command execution", "-c"},
		{"error output", ">&2"},
		{"NO_COLOR in completion", "NO_COLOR=1 command camp complete"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(output, check.content) {
				t.Errorf("zsh init missing %s: %q", check.name, check.content)
			}
		})
	}
}

func TestGenerateZsh_ValidSyntax(t *testing.T) {
	output := generateZsh()

	// Check for balanced braces (important for function definitions)
	braceCount := 0
	for _, c := range output {
		switch c {
		case '{':
			braceCount++
		case '}':
			braceCount--
		}
	}
	if braceCount != 0 {
		t.Errorf("Unbalanced braces in zsh output: %d", braceCount)
	}

	// Note: We don't check parentheses balance because zsh has special
	// syntax like (( )) for arithmetic and $() for command substitution
	// that makes simple counting unreliable.
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		shell   string
		wantErr bool
	}{
		{"zsh", false},
		{"bash", false},
		{"fish", false},
		{"unknown", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			output, err := Generate(tt.shell)
			if (err != nil) != tt.wantErr {
				t.Errorf("Generate(%q) error = %v, wantErr %v", tt.shell, err, tt.wantErr)
				return
			}
			if !tt.wantErr && output == "" {
				t.Errorf("Generate(%q) returned empty output", tt.shell)
			}
		})
	}
}

func TestIsSupported(t *testing.T) {
	tests := []struct {
		shell string
		want  bool
	}{
		{"zsh", true},
		{"bash", true},
		{"fish", true},
		{"sh", false},
		{"ksh", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			if got := IsSupported(tt.shell); got != tt.want {
				t.Errorf("IsSupported(%q) = %v, want %v", tt.shell, got, tt.want)
			}
		})
	}
}

func TestSupportedShells(t *testing.T) {
	if len(SupportedShells) == 0 {
		t.Error("SupportedShells should not be empty")
	}

	// All listed shells should be supported
	for _, shell := range SupportedShells {
		if !IsSupported(shell) {
			t.Errorf("Listed shell %q not supported", shell)
		}
	}
}

func TestGenerateZsh_ContainsCategoryShortcuts(t *testing.T) {
	output := generateZsh()

	// Verify shortcuts are generated from actual defaults, not hardcoded
	defaults := config.DefaultNavigationShortcuts()
	for key, sc := range defaults {
		if !sc.IsNavigation() {
			continue
		}
		if !strings.Contains(output, "'"+key+":") {
			t.Errorf("zsh init missing navigation shortcut: %s", key)
		}
	}
}

func TestGenerateZsh_ContainsCommands(t *testing.T) {
	output := generateZsh()

	commands := []string{
		"'init:Initialize",
		"'go:Navigate",
		"'switch:Switch",
		"'project:Manage",
		"'shell-init:Output",
	}

	for _, cmd := range commands {
		if !strings.Contains(output, cmd) {
			t.Errorf("zsh init missing command completion: %s", cmd)
		}
	}
}

func TestGenerateZsh_HasHeader(t *testing.T) {
	output := generateZsh()

	if !strings.HasPrefix(output, "# Camp CLI") {
		t.Error("zsh init should start with header comment")
	}

	if !strings.Contains(output, "eval \"$(camp shell-init zsh)\"") {
		t.Error("zsh init should contain setup instructions")
	}
}

func TestGenerateBash_Basic(t *testing.T) {
	output := generateBash()

	if output == "" {
		t.Fatal("generateBash() returned empty string")
	}

	// Check for cgo function
	if !strings.Contains(output, "cgo()") {
		t.Error("bash init missing cgo function")
	}
}

func TestGenerateFish_Basic(t *testing.T) {
	output := generateFish()

	if output == "" {
		t.Fatal("generateFish() returned empty string")
	}

	// Check for cgo function
	if !strings.Contains(output, "function cgo") {
		t.Error("fish init missing cgo function")
	}
}

// Benchmarks

func BenchmarkGenerateZsh(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = generateZsh()
	}
}

func BenchmarkGenerate(b *testing.B) {
	shells := []string{"zsh", "bash", "fish"}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = Generate(shells[i%len(shells)])
	}
}
