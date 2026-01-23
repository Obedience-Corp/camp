package dungeon

import (
	"strings"
	"testing"
)

func TestGetOBEYTemplate(t *testing.T) {
	content, err := GetOBEYTemplate()
	if err != nil {
		t.Fatalf("GetOBEYTemplate failed: %v", err)
	}

	if len(content) == 0 {
		t.Error("OBEY template should not be empty")
	}

	// Verify it contains expected content
	contentStr := string(content)
	if !strings.Contains(contentStr, "# Dungeon") {
		t.Error("OBEY template should contain '# Dungeon' header")
	}

	if !strings.Contains(contentStr, "archived/") {
		t.Error("OBEY template should mention archived/ directory")
	}
}

func TestGetArchivedREADME(t *testing.T) {
	content, err := GetArchivedREADME()
	if err != nil {
		t.Fatalf("GetArchivedREADME failed: %v", err)
	}

	if len(content) == 0 {
		t.Error("archived README should not be empty")
	}

	// Verify it contains expected content
	contentStr := string(content)
	if !strings.Contains(contentStr, "# Archived") {
		t.Error("archived README should contain '# Archived' header")
	}

	if !strings.Contains(contentStr, "git") {
		t.Error("archived README should mention git for recovery")
	}
}
