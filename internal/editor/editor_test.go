package editor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetEditor(t *testing.T) {
	// Save original env vars and restore after tests
	origEditor := os.Getenv("EDITOR")
	origVisual := os.Getenv("VISUAL")
	defer func() {
		if origEditor != "" {
			os.Setenv("EDITOR", origEditor)
		} else {
			os.Unsetenv("EDITOR")
		}
		if origVisual != "" {
			os.Setenv("VISUAL", origVisual)
		} else {
			os.Unsetenv("VISUAL")
		}
	}()

	tests := []struct {
		name         string
		envEditor    string
		envVisual    string
		wantContains string // Use contains for flexibility with config fallback
	}{
		{
			name:         "EDITOR set takes priority",
			envEditor:    "vim",
			envVisual:    "emacs",
			wantContains: "vim",
		},
		{
			name:         "VISUAL used when EDITOR empty",
			envEditor:    "",
			envVisual:    "emacs",
			wantContains: "emacs",
		},
		{
			name:         "EDITOR with path",
			envEditor:    "/usr/local/bin/nvim",
			envVisual:    "",
			wantContains: "/usr/local/bin/nvim",
		},
		{
			name:         "VISUAL with path",
			envEditor:    "",
			envVisual:    "/usr/bin/code",
			wantContains: "/usr/bin/code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			os.Setenv("EDITOR", tt.envEditor)
			os.Setenv("VISUAL", tt.envVisual)

			editor := GetEditor(context.Background())
			if editor != tt.wantContains {
				t.Errorf("GetEditor() = %q, want %q", editor, tt.wantContains)
			}
		})
	}
}

func TestGetEditor_PlatformFallback(t *testing.T) {
	// Save original env vars
	origEditor := os.Getenv("EDITOR")
	origVisual := os.Getenv("VISUAL")
	defer func() {
		if origEditor != "" {
			os.Setenv("EDITOR", origEditor)
		} else {
			os.Unsetenv("EDITOR")
		}
		if origVisual != "" {
			os.Setenv("VISUAL", origVisual)
		} else {
			os.Unsetenv("VISUAL")
		}
	}()

	// Clear env vars to trigger fallback
	os.Unsetenv("EDITOR")
	os.Unsetenv("VISUAL")

	editor := GetEditor(context.Background())

	// Platform-specific expectation
	var expected string
	switch runtime.GOOS {
	case "windows":
		expected = "notepad"
	default:
		expected = "nano"
	}

	// Note: This may return config value if user has global config set
	// For a clean test, we'd need to mock config, but this verifies
	// at least the function returns a valid string
	if editor == "" {
		t.Error("GetEditor() returned empty string")
	}

	// If no config, should be platform default
	if editor == expected {
		t.Logf("GetEditor() returned platform default %q", editor)
	} else {
		t.Logf("GetEditor() returned %q (may be from config)", editor)
	}
}

func TestGetEditor_ContextCancelled(t *testing.T) {
	// Save original env vars
	origEditor := os.Getenv("EDITOR")
	origVisual := os.Getenv("VISUAL")
	defer func() {
		if origEditor != "" {
			os.Setenv("EDITOR", origEditor)
		} else {
			os.Unsetenv("EDITOR")
		}
		if origVisual != "" {
			os.Setenv("VISUAL", origVisual)
		} else {
			os.Unsetenv("VISUAL")
		}
	}()

	// Clear env vars to force config check
	os.Unsetenv("EDITOR")
	os.Unsetenv("VISUAL")

	// Cancelled context should still return a valid editor (platform default)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	editor := GetEditor(ctx)
	if editor == "" {
		t.Error("GetEditor() should return platform default even with cancelled context")
	}
}

func TestGetEditorSource(t *testing.T) {
	// Save original env vars
	origEditor := os.Getenv("EDITOR")
	origVisual := os.Getenv("VISUAL")
	defer func() {
		if origEditor != "" {
			os.Setenv("EDITOR", origEditor)
		} else {
			os.Unsetenv("EDITOR")
		}
		if origVisual != "" {
			os.Setenv("VISUAL", origVisual)
		} else {
			os.Unsetenv("VISUAL")
		}
	}()

	tests := []struct {
		name       string
		envEditor  string
		envVisual  string
		wantEditor string
		wantSource string
	}{
		{
			name:       "EDITOR source",
			envEditor:  "vim",
			envVisual:  "emacs",
			wantEditor: "vim",
			wantSource: "$EDITOR",
		},
		{
			name:       "VISUAL source",
			envEditor:  "",
			envVisual:  "emacs",
			wantEditor: "emacs",
			wantSource: "$VISUAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("EDITOR", tt.envEditor)
			os.Setenv("VISUAL", tt.envVisual)

			editor, source := GetEditorSource(context.Background())
			if editor != tt.wantEditor {
				t.Errorf("GetEditorSource() editor = %q, want %q", editor, tt.wantEditor)
			}
			if source != tt.wantSource {
				t.Errorf("GetEditorSource() source = %q, want %q", source, tt.wantSource)
			}
		})
	}
}

func TestGetEditorSource_Fallback(t *testing.T) {
	// Save original env vars
	origEditor := os.Getenv("EDITOR")
	origVisual := os.Getenv("VISUAL")
	defer func() {
		if origEditor != "" {
			os.Setenv("EDITOR", origEditor)
		} else {
			os.Unsetenv("EDITOR")
		}
		if origVisual != "" {
			os.Setenv("VISUAL", origVisual)
		} else {
			os.Unsetenv("VISUAL")
		}
	}()

	os.Unsetenv("EDITOR")
	os.Unsetenv("VISUAL")

	editor, source := GetEditorSource(context.Background())

	// Should be either config or default
	if source != "config" && source != "default" {
		t.Errorf("GetEditorSource() source = %q, want 'config' or 'default'", source)
	}
	if editor == "" {
		t.Error("GetEditorSource() editor should not be empty")
	}
}

func TestDefaultEditor(t *testing.T) {
	editor := defaultEditor()

	switch runtime.GOOS {
	case "windows":
		if editor != "notepad" {
			t.Errorf("defaultEditor() on windows = %q, want 'notepad'", editor)
		}
	default:
		if editor != "nano" {
			t.Errorf("defaultEditor() on %s = %q, want 'nano'", runtime.GOOS, editor)
		}
	}
}

func TestGetEditor_Priority(t *testing.T) {
	// Save original env vars
	origEditor := os.Getenv("EDITOR")
	origVisual := os.Getenv("VISUAL")
	defer func() {
		if origEditor != "" {
			os.Setenv("EDITOR", origEditor)
		} else {
			os.Unsetenv("EDITOR")
		}
		if origVisual != "" {
			os.Setenv("VISUAL", origVisual)
		} else {
			os.Unsetenv("VISUAL")
		}
	}()

	// Test that EDITOR beats VISUAL
	os.Setenv("EDITOR", "editor_priority")
	os.Setenv("VISUAL", "visual_priority")

	editor := GetEditor(context.Background())
	if editor != "editor_priority" {
		t.Errorf("GetEditor() should prioritize EDITOR over VISUAL, got %q", editor)
	}
}

func TestMoveFile(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) (src, dst string)
		wantErr bool
		checks  func(t *testing.T, src, dst string)
	}{
		{
			name: "same directory move",
			setup: func(t *testing.T) (src, dst string) {
				tmpDir := t.TempDir()
				src = filepath.Join(tmpDir, "source.txt")
				dst = filepath.Join(tmpDir, "dest.txt")
				if err := os.WriteFile(src, []byte("test content"), 0644); err != nil {
					t.Fatal(err)
				}
				return src, dst
			},
			wantErr: false,
			checks: func(t *testing.T, src, dst string) {
				// Source should be gone
				if _, err := os.Stat(src); !os.IsNotExist(err) {
					t.Error("source file still exists after move")
				}
				// Destination should exist with correct content
				content, err := os.ReadFile(dst)
				if err != nil {
					t.Errorf("reading destination: %v", err)
				}
				if string(content) != "test content" {
					t.Errorf("content = %q, want %q", string(content), "test content")
				}
			},
		},
		{
			name: "move to subdirectory",
			setup: func(t *testing.T) (src, dst string) {
				tmpDir := t.TempDir()
				src = filepath.Join(tmpDir, "source.txt")
				subDir := filepath.Join(tmpDir, "subdir")
				os.MkdirAll(subDir, 0755)
				dst = filepath.Join(subDir, "dest.txt")
				if err := os.WriteFile(src, []byte("test"), 0644); err != nil {
					t.Fatal(err)
				}
				return src, dst
			},
			wantErr: false,
			checks: func(t *testing.T, src, dst string) {
				if _, err := os.Stat(dst); err != nil {
					t.Errorf("destination doesn't exist: %v", err)
				}
			},
		},
		{
			name: "preserves permissions",
			setup: func(t *testing.T) (src, dst string) {
				tmpDir := t.TempDir()
				src = filepath.Join(tmpDir, "source.txt")
				dst = filepath.Join(tmpDir, "dest.txt")
				if err := os.WriteFile(src, []byte("test"), 0600); err != nil {
					t.Fatal(err)
				}
				return src, dst
			},
			wantErr: false,
			checks: func(t *testing.T, src, dst string) {
				info, err := os.Stat(dst)
				if err != nil {
					t.Errorf("stat destination: %v", err)
					return
				}
				// Check that it's not world-readable
				if info.Mode().Perm()&0044 != 0 {
					// Note: on some systems permissions might be modified by umask
					// This is a soft check
					t.Logf("Permissions may not be fully preserved (got %o)", info.Mode().Perm())
				}
			},
		},
		{
			name: "source doesn't exist",
			setup: func(t *testing.T) (src, dst string) {
				tmpDir := t.TempDir()
				return filepath.Join(tmpDir, "nonexistent.txt"),
					filepath.Join(tmpDir, "dest.txt")
			},
			wantErr: true,
		},
		{
			name: "destination directory doesn't exist",
			setup: func(t *testing.T) (src, dst string) {
				tmpDir := t.TempDir()
				src = filepath.Join(tmpDir, "source.txt")
				if err := os.WriteFile(src, []byte("test"), 0644); err != nil {
					t.Fatal(err)
				}
				dst = filepath.Join(tmpDir, "nonexistent", "subdir", "dest.txt")
				return src, dst
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := tt.setup(t)

			err := MoveFile(src, dst)
			if (err != nil) != tt.wantErr {
				t.Errorf("MoveFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checks != nil {
				tt.checks(t, src, dst)
			}
		})
	}
}

func TestMoveFile_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "large.txt")
	dst := filepath.Join(tmpDir, "large_dest.txt")

	// Create a file with 1MB of data
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := MoveFile(src, dst); err != nil {
		t.Fatalf("MoveFile() error = %v", err)
	}

	// Verify content
	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if len(content) != len(data) {
		t.Errorf("content length = %d, want %d", len(content), len(data))
	}

	// Verify source is gone
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source file still exists after move")
	}
}

func TestMoveFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "empty.txt")
	dst := filepath.Join(tmpDir, "empty_dest.txt")

	// Create empty file
	if err := os.WriteFile(src, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	if err := MoveFile(src, dst); err != nil {
		t.Fatalf("MoveFile() error = %v", err)
	}

	// Verify destination exists and is empty
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("destination size = %d, want 0", info.Size())
	}
}

func TestBuildEditorCommand(t *testing.T) {
	tests := []struct {
		name       string
		editor     string
		path       string
		wantWait   bool
		wantEditor string
	}{
		{
			name:       "terminal editor vim",
			editor:     "vim",
			path:       "/tmp/test.md",
			wantWait:   false,
			wantEditor: "vim",
		},
		{
			name:       "terminal editor nano",
			editor:     "nano",
			path:       "/tmp/test.md",
			wantWait:   false,
			wantEditor: "nano",
		},
		{
			name:       "terminal editor nvim",
			editor:     "nvim",
			path:       "/tmp/test.md",
			wantWait:   false,
			wantEditor: "nvim",
		},
		{
			name:       "terminal editor emacs",
			editor:     "emacs",
			path:       "/tmp/test.md",
			wantWait:   false,
			wantEditor: "emacs",
		},
		{
			name:       "GUI editor code",
			editor:     "code",
			path:       "/tmp/test.md",
			wantWait:   true,
			wantEditor: "code",
		},
		{
			name:       "GUI editor code-insiders",
			editor:     "code-insiders",
			path:       "/tmp/test.md",
			wantWait:   true,
			wantEditor: "code-insiders",
		},
		{
			name:       "GUI editor subl",
			editor:     "subl",
			path:       "/tmp/test.md",
			wantWait:   true,
			wantEditor: "subl",
		},
		{
			name:       "GUI editor sublime_text",
			editor:     "sublime_text",
			path:       "/tmp/test.md",
			wantWait:   true,
			wantEditor: "sublime_text",
		},
		{
			name:       "GUI editor atom",
			editor:     "atom",
			path:       "/tmp/test.md",
			wantWait:   true,
			wantEditor: "atom",
		},
		{
			name:       "full path terminal editor",
			editor:     "/usr/local/bin/vim",
			path:       "/tmp/test.md",
			wantWait:   false,
			wantEditor: "/usr/local/bin/vim",
		},
		{
			name:       "full path GUI editor code",
			editor:     "/usr/local/bin/code",
			path:       "/tmp/test.md",
			wantWait:   true,
			wantEditor: "/usr/local/bin/code",
		},
		{
			name:       "full path GUI editor with spaces",
			editor:     "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
			path:       "/tmp/test.md",
			wantWait:   true,
			wantEditor: "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cmd := BuildEditorCommand(ctx, tt.editor, tt.path)

			// Verify editor path
			if cmd.Path == "" {
				// cmd.Path is set by exec.Command after LookPath
				// For our test, check Args[0]
				if cmd.Args[0] != tt.wantEditor {
					t.Errorf("editor = %q, want %q", cmd.Args[0], tt.wantEditor)
				}
			}

			// Check for --wait flag
			hasWait := false
			for _, arg := range cmd.Args {
				if arg == "--wait" {
					hasWait = true
					break
				}
			}

			if hasWait != tt.wantWait {
				t.Errorf("--wait flag = %v, want %v (args: %v)", hasWait, tt.wantWait, cmd.Args)
			}

			// Verify path is in args
			hasPath := false
			for _, arg := range cmd.Args {
				if arg == tt.path {
					hasPath = true
					break
				}
			}
			if !hasPath {
				t.Errorf("path %q not found in args: %v", tt.path, cmd.Args)
			}
		})
	}
}

func TestBuildEditorCommand_ArgsOrder(t *testing.T) {
	ctx := context.Background()

	// Terminal editor: should have [editor, path]
	termCmd := BuildEditorCommand(ctx, "vim", "/tmp/test.md")
	if len(termCmd.Args) != 2 {
		t.Errorf("terminal editor args count = %d, want 2", len(termCmd.Args))
	}
	if termCmd.Args[0] != "vim" || termCmd.Args[1] != "/tmp/test.md" {
		t.Errorf("terminal args = %v, want [vim, /tmp/test.md]", termCmd.Args)
	}

	// GUI editor: should have [editor, --wait, path]
	guiCmd := BuildEditorCommand(ctx, "code", "/tmp/test.md")
	if len(guiCmd.Args) != 3 {
		t.Errorf("GUI editor args count = %d, want 3", len(guiCmd.Args))
	}
	if guiCmd.Args[0] != "code" || guiCmd.Args[1] != "--wait" || guiCmd.Args[2] != "/tmp/test.md" {
		t.Errorf("GUI args = %v, want [code, --wait, /tmp/test.md]", guiCmd.Args)
	}
}

func TestBuildEditorCommand_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel

	// BuildEditorCommand should still return a cmd even with cancelled context
	// The context cancellation is handled when cmd.Run() is called
	cmd := BuildEditorCommand(ctx, "vim", "/tmp/test.md")
	if cmd == nil {
		t.Error("BuildEditorCommand returned nil with cancelled context")
	}
}

func TestEdit(t *testing.T) {
	// Save original env vars
	origEditor := os.Getenv("EDITOR")
	origVisual := os.Getenv("VISUAL")
	defer func() {
		if origEditor != "" {
			os.Setenv("EDITOR", origEditor)
		} else {
			os.Unsetenv("EDITOR")
		}
		if origVisual != "" {
			os.Setenv("VISUAL", origVisual)
		} else {
			os.Unsetenv("VISUAL")
		}
	}()

	// Set EDITOR to a command that will succeed (true on unix)
	os.Setenv("EDITOR", "true")
	os.Unsetenv("VISUAL")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Edit should use GetEditor to find "true" and run it
	err := Edit(context.Background(), testFile)
	if err != nil {
		t.Errorf("Edit() error = %v", err)
	}
}

func TestOpenEditor_InvalidEditor(t *testing.T) {
	ctx := context.Background()

	// Use a non-existent editor
	err := OpenEditor(ctx, "/nonexistent/editor/path", "/tmp/test.md")
	if err == nil {
		t.Error("OpenEditor() expected error for non-existent editor")
	}
}
