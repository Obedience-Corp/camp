package shell

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestGenerateFish(t *testing.T) {
	output := generateFish()

	if output == "" {
		t.Fatal("generateFish() returned empty string")
	}

	// Check for essential components
	checks := []struct {
		name    string
		content string
	}{
		{"cgo function", "function cgo"},
		{"cint function", "function cint"},
		{"cint calls intent add", "camp intent add"},
		{"cd command", "cd $dest"},
		{"camp go call", "camp go"},
		{"complete command", "complete -c cgo"},
		{"camp completion", "complete -c camp"},
		{"command execution", "-c"},
		{"error output", ">&2"},
		{"fish test syntax", "test (count"},
		{"fish set syntax", "set -l"},
		{"NO_COLOR in completion", "NO_COLOR=1 command camp complete"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(output, check.content) {
				t.Errorf("fish init missing %s: %q", check.name, check.content)
			}
		})
	}
}

func TestGenerateFish_ValidSyntax(t *testing.T) {
	// Skip if fish is not available
	if _, err := exec.LookPath("fish"); err != nil {
		t.Skip("fish not available")
	}

	script := generateFish()

	// Write to temp file
	f, err := os.CreateTemp("", "camp-fish-*.fish")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(script); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}
	f.Close()

	// Check syntax with fish -n
	cmd := exec.Command("fish", "-n", f.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Fish syntax error: %s\n%v", output, err)
	}
}

func TestGenerateFish_NoPosixSyntax(t *testing.T) {
	output := generateFish()

	// Check that fish init doesn't use bash/POSIX syntax
	posixPatterns := []struct {
		name    string
		pattern string
	}{
		// Note: We can't check for `[ ` because fish uses it in arrays like [2..-1]
		{"bash function syntax", "() {"},
		{"bash if syntax", "if ["},
		{"bash then", "\nthen"},
		{"fi keyword", "\nfi"},
		{"done keyword", "\ndone"},
	}

	for _, check := range posixPatterns {
		t.Run(check.name, func(t *testing.T) {
			if strings.Contains(output, check.pattern) {
				t.Errorf("fish init uses POSIX %s: found %q", check.name, check.pattern)
			}
		})
	}
}

func TestGenerateFish_UsesFishSyntax(t *testing.T) {
	output := generateFish()

	// Check for fish-specific syntax
	fishPatterns := []string{
		"function ",
		"end",
		"set -l",
		"test ",
		"if test",
	}

	for _, pattern := range fishPatterns {
		if !strings.Contains(output, pattern) {
			t.Errorf("fish init missing fish syntax: %q", pattern)
		}
	}
}

func TestGenerateFish_ContainsCategoryDescriptions(t *testing.T) {
	output := generateFish()

	// Verify shortcuts are generated from actual defaults, not hardcoded
	defaults := config.DefaultNavigationShortcuts()
	for key, sc := range defaults {
		if !sc.IsNavigation() {
			continue
		}
		// Fish format: complete -c cgo -n "__camp_is_first_arg" -a "key" -d "description"
		if !strings.Contains(output, "\""+key+"\"") {
			t.Errorf("fish init missing shortcut: %s", key)
		}
	}
}

func TestGenerateFish_ContainsCommands(t *testing.T) {
	output := generateFish()

	commands := []string{
		"\"init\"",
		"\"go\"",
		"\"project\"",
		"\"shell-init\"",
		"\"version\"",
	}

	for _, cmd := range commands {
		if !strings.Contains(output, cmd) {
			t.Errorf("fish init missing command completion: %s", cmd)
		}
	}
}

func TestGenerateFish_HasHeader(t *testing.T) {
	output := generateFish()

	if !strings.HasPrefix(output, "# Camp CLI") {
		t.Error("fish init should start with header comment")
	}

	if !strings.Contains(output, "camp shell-init fish | source") {
		t.Error("fish init should contain setup instructions")
	}
}

func TestGenerateFish_HasCommandCheck(t *testing.T) {
	output := generateFish()

	// Should check if camp command exists
	if !strings.Contains(output, "command -v camp") {
		t.Error("fish init should check if camp command exists")
	}
}

func TestGenerateFish_HasHelperFunction(t *testing.T) {
	output := generateFish()

	// Should have the helper function for first argument detection
	if !strings.Contains(output, "__camp_is_first_arg") {
		t.Error("fish init should have __camp_is_first_arg helper")
	}
}

// Benchmark

func BenchmarkGenerateFish(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = generateFish()
	}
}
