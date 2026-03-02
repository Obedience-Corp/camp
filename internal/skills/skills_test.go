package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

// resolvePath resolves symlinks for consistent path comparison on macOS.
func resolvePath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("resolve path %s: %v", p, err)
	}
	return resolved
}

// setupCampaign creates a temporary campaign directory structure for testing.
// Returns the resolved campaign root path.
func setupCampaign(t *testing.T) string {
	t.Helper()
	tmpDir := resolvePath(t, t.TempDir())

	campaignRoot := filepath.Join(tmpDir, "my-campaign")
	skillsDir := filepath.Join(campaignRoot, ".campaign", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("create skills dir: %v", err)
	}
	return campaignRoot
}

func TestFindSkillsDir(t *testing.T) {
	campaignRoot := setupCampaign(t)

	// Clear cache and set env override for campaign detection
	campaign.ClearCache()
	t.Setenv(campaign.EnvCampaignRoot, campaignRoot)
	t.Setenv(campaign.EnvCacheDisable, "1")

	ctx := context.Background()
	got, err := FindSkillsDir(ctx)
	if err != nil {
		t.Fatalf("FindSkillsDir: %v", err)
	}

	want := filepath.Join(campaignRoot, ".campaign", "skills")
	if got != want {
		t.Errorf("FindSkillsDir = %q, want %q", got, want)
	}
}

func TestFindSkillsDir_NotInCampaign(t *testing.T) {
	tmpDir := resolvePath(t, t.TempDir())

	campaign.ClearCache()
	t.Setenv(campaign.EnvCampaignRoot, "")
	t.Setenv(campaign.EnvCacheDisable, "1")

	// Change to a directory with no campaign
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	ctx := context.Background()
	_, err := FindSkillsDir(ctx)
	if err == nil {
		t.Fatal("FindSkillsDir should have returned error when not in campaign")
	}
}

func TestFindSkillsDir_NoSkillsDir(t *testing.T) {
	tmpDir := resolvePath(t, t.TempDir())

	// Create campaign root without skills directory
	campaignDir := filepath.Join(tmpDir, ".campaign")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("create campaign dir: %v", err)
	}

	campaign.ClearCache()
	t.Setenv(campaign.EnvCampaignRoot, tmpDir)
	t.Setenv(campaign.EnvCacheDisable, "1")

	ctx := context.Background()
	_, err := FindSkillsDir(ctx)
	if err == nil {
		t.Fatal("FindSkillsDir should have returned error when skills dir missing")
	}
}

func TestRelativeSymlinkTarget(t *testing.T) {
	tests := []struct {
		name     string
		linkPath string
		target   string
		want     string
	}{
		{
			name:     "sibling directory",
			linkPath: "/project/.claude/skills",
			target:   "/project/.campaign/skills",
			want:     filepath.Join("..", ".campaign", "skills"),
		},
		{
			name:     "deeply nested link",
			linkPath: "/project/a/b/c/skills",
			target:   "/project/.campaign/skills",
			want:     filepath.Join("..", "..", "..", ".campaign", "skills"),
		},
		{
			name:     "same directory",
			linkPath: "/project/.campaign/link",
			target:   "/project/.campaign/skills",
			want:     "skills",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RelativeSymlinkTarget(tt.linkPath, tt.target)
			if err != nil {
				t.Fatalf("RelativeSymlinkTarget: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckLinkState(t *testing.T) {
	tmpDir := resolvePath(t, t.TempDir())

	// Create a real target directory
	targetDir := filepath.Join(tmpDir, "target")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a valid symlink
	validLink := filepath.Join(tmpDir, "valid-link")
	if err := os.Symlink(targetDir, validLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Create a broken symlink
	brokenLink := filepath.Join(tmpDir, "broken-link")
	if err := os.Symlink(filepath.Join(tmpDir, "nonexistent"), brokenLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Create a regular file (not a link)
	regularFile := filepath.Join(tmpDir, "regular")
	if err := os.WriteFile(regularFile, []byte("data"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		expectedTarget string
		want           LinkState
	}{
		{
			name:           "valid symlink with matching target",
			path:           validLink,
			expectedTarget: targetDir,
			want:           StateValid,
		},
		{
			name:           "valid symlink with empty expected target",
			path:           validLink,
			expectedTarget: "",
			want:           StateValid,
		},
		{
			name:           "valid symlink with wrong target",
			path:           validLink,
			expectedTarget: filepath.Join(tmpDir, "other"),
			want:           StateBroken,
		},
		{
			name:           "broken symlink",
			path:           brokenLink,
			expectedTarget: targetDir,
			want:           StateBroken,
		},
		{
			name:           "regular file",
			path:           regularFile,
			expectedTarget: "",
			want:           StateNotALink,
		},
		{
			name:           "missing path",
			path:           filepath.Join(tmpDir, "does-not-exist"),
			expectedTarget: "",
			want:           StateMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckLinkState(tt.path, tt.expectedTarget)
			if err != nil {
				t.Fatalf("CheckLinkState: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveToolPath(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		want    string
		wantErr bool
	}{
		{name: "claude", tool: "claude", want: ".claude/skills"},
		{name: "agents", tool: "agents", want: ".agents/skills"},
		{name: "unknown tool", tool: "vscode", wantErr: true},
		{name: "empty tool", tool: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveToolPath(tt.tool)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveToolPath: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateDestination(t *testing.T) {
	rootBase := resolvePath(t, t.TempDir())
	root := filepath.Join(rootBase, "mycamp")
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "skills"), 0755); err != nil {
		t.Fatalf("create campaign root: %v", err)
	}

	outside := resolvePath(t, t.TempDir())

	// Symlink inside campaign that points outside campaign root.
	escapeLink := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escapeLink); err != nil {
		t.Fatalf("create escape symlink: %v", err)
	}

	// Symlink inside campaign that points to another in-root directory.
	inRootTarget := filepath.Join(root, "internal-target")
	if err := os.MkdirAll(inRootTarget, 0755); err != nil {
		t.Fatalf("create in-root target: %v", err)
	}
	inRootLink := filepath.Join(root, "internal-link")
	if err := os.Symlink(inRootTarget, inRootLink); err != nil {
		t.Fatalf("create in-root symlink: %v", err)
	}

	tests := []struct {
		name    string
		dest    string
		wantErr bool
	}{
		{name: "valid subpath", dest: filepath.Join(root, ".claude", "skills"), wantErr: false},
		{name: "valid nested subpath", dest: filepath.Join(root, "tools", "custom", "skills"), wantErr: false},
		{name: "valid in-root symlink parent", dest: filepath.Join(inRootLink, "skills"), wantErr: false},
		{name: "campaign root", dest: root, wantErr: true},
		{name: "campaign root with trailing slash", dest: root + string(filepath.Separator), wantErr: true},
		{name: "dot path resolves to root", dest: filepath.Join(root, "."), wantErr: true},
		{name: ".campaign dir", dest: filepath.Join(root, ".campaign"), wantErr: true},
		{name: ".campaign subdir", dest: filepath.Join(root, ".campaign", "skills"), wantErr: true},
		{name: "filesystem root", dest: string(filepath.Separator), wantErr: true},
		{name: "outside campaign", dest: outside, wantErr: true},
		{name: "parent of campaign", dest: filepath.Dir(root), wantErr: true},
		{name: "symlink parent escapes campaign", dest: filepath.Join(escapeLink, "customskills"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDestination(tt.dest, root)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for dest=%q, got nil", tt.dest)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for dest=%q: %v", tt.dest, err)
			}
		})
	}
}

func TestCheckPathType(t *testing.T) {
	tmpDir := resolvePath(t, t.TempDir())

	// Create a directory
	dir := filepath.Join(tmpDir, "mydir")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a regular file
	file := filepath.Join(tmpDir, "myfile")
	if err := os.WriteFile(file, []byte("data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Create a symlink
	link := filepath.Join(tmpDir, "mylink")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	tests := []struct {
		name string
		path string
		want PathType
	}{
		{name: "directory", path: dir, want: TypeDirectory},
		{name: "file", path: file, want: TypeFile},
		{name: "symlink", path: link, want: TypeSymlink},
		{name: "missing", path: filepath.Join(tmpDir, "nope"), want: TypeMissing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckPathType(tt.path)
			if err != nil {
				t.Fatalf("CheckPathType: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
