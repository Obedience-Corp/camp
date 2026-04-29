package shell

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestGenerateBash(t *testing.T) {
	output := generateBash()

	if output == "" {
		t.Fatal("generateBash() returned empty string")
	}

	// Check for essential components
	checks := []struct {
		name    string
		content string
	}{
		{"camp wrapper function", "camp()"},
		{"cgo function", "cgo()"},
		{"cint function", "cint()"},
		{"cint calls intent add", "camp intent add"},
		{"cd command", "cd \"$dest\""},
		{"camp go call", "camp go"},
		{"command camp binary call", "command camp"},
		{"switch subcommand", "switch|sw)"},
		{"go subcommand", "go|g)"},
		{"workitem subcommand", "workitem|wi|workitems)"},
		{"workitem path output", "--path-output"},
		{"workitem root resolution", "command camp root"},
		{"workitem temp file", "mktemp"},
		{"complete builtin", "complete -F"},
		{"cgo completion", "_cgo_complete"},
		{"camp completion", "_camp_complete"},
		{"command execution", "-c"},
		{"error output", ">&2"},
		{"NO_COLOR in completion", "NO_COLOR=1 command camp complete"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(output, check.content) {
				t.Errorf("bash init missing %s: %q", check.name, check.content)
			}
		})
	}
}

func TestGenerateBash_ContainsDynamicShortcuts(t *testing.T) {
	output := generateBash()

	// Verify shortcuts come from actual defaults
	defaults := config.DefaultNavigationShortcuts()
	for key, sc := range defaults {
		if !sc.IsNavigation() {
			continue
		}
		if !strings.Contains(output, key) {
			t.Errorf("bash init missing navigation shortcut key: %s", key)
		}
	}
}

func TestGenerateBash_ValidSyntax(t *testing.T) {
	// Skip if bash is not available
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	script := generateBash()

	// Write to temp file
	f, err := os.CreateTemp("", "camp-bash-*.sh")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(script); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}
	f.Close()

	// Check syntax with bash -n
	cmd := exec.Command("bash", "-n", f.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Bash syntax error: %s\n%v", output, err)
	}
}

func TestGenerateBash_Bash32Compatible(t *testing.T) {
	output := generateBash()

	// Check for features not available in bash 3.2
	incompatible := []struct {
		name    string
		pattern string
	}{
		{"associative arrays", "declare -A"},
		// ${var,,} is lowercase expansion (not just ${var})
		{"lowercase expansion", ",,}"},
		{"uppercase expansion", "^^}"},
		{"double bracket test", "[[ "},
	}

	for _, check := range incompatible {
		t.Run(check.name, func(t *testing.T) {
			// Skip comments when checking
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "#") {
					continue
				}
				if check.pattern == "[[ " && strings.Contains(line, "[[") {
					t.Errorf("bash init uses %s which is not bash 3.2 compatible", check.name)
				}
				if check.pattern != "[[ " && strings.Contains(line, check.pattern) {
					t.Errorf("bash init uses %s which is not bash 3.2 compatible", check.name)
				}
			}
		})
	}
}

func TestGenerateBash_UsesPosixTest(t *testing.T) {
	output := generateBash()

	// Should use [ ] for tests (POSIX compatible)
	if !strings.Contains(output, "[ $#") {
		t.Error("bash init should use POSIX [ ] test for argument count")
	}

	if !strings.Contains(output, "[ -n") {
		t.Error("bash init should use POSIX [ ] test for string check")
	}
}

func TestGenerateBash_ContainsCommands(t *testing.T) {
	output := generateBash()

	commands := []string{
		"init",
		"go",
		"switch",
		"project",
		"shell-init",
	}

	for _, cmd := range commands {
		if !strings.Contains(output, cmd) {
			t.Errorf("bash init missing command: %s", cmd)
		}
	}
}

func TestGenerateBash_HasHeader(t *testing.T) {
	output := generateBash()

	if !strings.HasPrefix(output, "# Camp CLI") {
		t.Error("bash init should start with header comment")
	}

	if !strings.Contains(output, "eval \"$(camp shell-init bash)\"") {
		t.Error("bash init should contain setup instructions")
	}
}

func TestGenerateBash_HasCommandCheck(t *testing.T) {
	output := generateBash()

	// Should check if camp command exists
	if !strings.Contains(output, "command -v camp") {
		t.Error("bash init should check if camp command exists")
	}
}

func TestGenerateBash_BalancedBraces(t *testing.T) {
	output := generateBash()

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
		t.Errorf("Unbalanced braces in bash output: %d", braceCount)
	}
}

// Benchmark

func BenchmarkGenerateBash(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = generateBash()
	}
}
