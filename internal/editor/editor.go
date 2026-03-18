// Package editor provides utilities for detecting and launching the user's
// preferred text editor for interactive editing workflows.
package editor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/obey-shared/procutil"
)

// GetEditor returns the user's preferred editor by checking (in order):
//  1. $EDITOR environment variable
//  2. $VISUAL environment variable
//  3. GlobalConfig.Editor from config.json
//  4. Platform-specific fallback (nano for Unix, notepad for Windows)
//
// The function silently ignores config loading errors and falls back to
// platform defaults, ensuring the editor workflow never fails due to config issues.
func GetEditor(ctx context.Context) string {
	// 1. Check EDITOR env var (highest priority)
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}

	// 2. Check VISUAL env var
	if visual := os.Getenv("VISUAL"); visual != "" {
		return visual
	}

	// 3. Check global config (ignore errors, use fallback)
	cfg, err := config.LoadGlobalConfig(ctx)
	if err == nil && cfg.Editor != "" {
		return cfg.Editor
	}

	// 4. Platform-specific fallback
	return defaultEditor()
}

// GetEditorSource returns both the editor command and the source where it was found.
// This is useful for displaying configuration info to users.
func GetEditorSource(ctx context.Context) (editor, source string) {
	// 1. Check EDITOR env var
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor, "$EDITOR"
	}

	// 2. Check VISUAL env var
	if visual := os.Getenv("VISUAL"); visual != "" {
		return visual, "$VISUAL"
	}

	// 3. Check global config
	cfg, err := config.LoadGlobalConfig(ctx)
	if err == nil && cfg.Editor != "" {
		return cfg.Editor, "config"
	}

	// 4. Platform-specific fallback
	return defaultEditor(), "default"
}

// defaultEditor returns the platform-specific default editor.
func defaultEditor() string {
	switch runtime.GOOS {
	case "windows":
		return "notepad"
	default:
		// nano is pre-installed on macOS and most Linux distributions
		return "nano"
	}
}

// MoveFile moves a file from src to dst, handling cross-filesystem moves.
//
// It first attempts a fast os.Rename (same filesystem). If that fails,
// it falls back to copying the file contents and deleting the source.
//
// On error during copy, any partial destination file is cleaned up.
// File permissions are preserved during the move.
//
// This function is designed for moving temporary intent files from /tmp
// to the campaign intents directory, which may be on different filesystems.
func MoveFile(src, dst string) error {
	// Try rename first (same filesystem - fast path)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Fall back to copy + delete (cross-filesystem)
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source file %q: %w", src, err)
	}
	defer srcFile.Close()

	// Get source file info for permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("getting source file info %q: %w", src, err)
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating destination file %q: %w", dst, err)
	}
	defer dstFile.Close()

	// Copy contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		os.Remove(dst) // Clean up partial copy
		return fmt.Errorf("copying file from %q to %q: %w", src, dst, err)
	}

	// Sync to ensure data is written
	if err := dstFile.Sync(); err != nil {
		os.Remove(dst) // Clean up on sync failure
		return fmt.Errorf("syncing destination file %q: %w", dst, err)
	}

	// Set permissions to match source
	if err := os.Chmod(dst, srcInfo.Mode()); err != nil {
		// Don't fail on chmod - file is copied successfully
		// Log in production, ignore in this implementation
	}

	// Remove source file after successful copy
	if err := os.Remove(src); err != nil {
		// Don't fail if we can't remove source - copy succeeded
		// Log in production, ignore in this implementation
	}

	return nil
}

// Supported Editors
//
// Terminal Editors (work without special flags):
//   - vim, nvim: Vi/Neovim
//   - nano: GNU nano
//   - emacs: GNU Emacs (terminal mode)
//   - emacsclient: Emacs client
//
// GUI Editors (require --wait flag):
//   - code: Visual Studio Code
//   - code-insiders: VS Code Insiders
//   - subl: Sublime Text
//   - sublime_text: Sublime Text (alternate name)
//   - atom: Atom editor
//
// Platform Defaults:
//   - Windows: notepad
//   - macOS/Linux: nano

// guiEditors contains editor names that require --wait flag to block.
var guiEditors = map[string]bool{
	"code":          true,
	"code-insiders": true,
	"subl":          true,
	"sublime_text":  true,
	"atom":          true,
}

// BuildEditorCommand constructs an exec.Cmd for launching the specified editor.
// For GUI editors that fork to background (VS Code, Sublime, Atom), it adds
// the --wait flag to make them block until the file is closed.
//
// The returned command intentionally does not modify process-group behavior.
// Bubble Tea's tea.ExecProcess needs the editor to stay in the foreground
// process group so terminal editors can take control of the TTY correctly.
// CLI callers that need subprocess cleanup should use OpenEditor, which applies
// process-group isolation via procutil.RunWithCleanup.
func BuildEditorCommand(ctx context.Context, editor, path string) *exec.Cmd {
	base := filepath.Base(editor)

	var cmd *exec.Cmd
	if guiEditors[base] {
		cmd = exec.CommandContext(ctx, editor, "--wait", path)
	} else {
		cmd = exec.CommandContext(ctx, editor, path)
	}

	return cmd
}

// OpenEditor opens the specified file in the user's editor and waits for editing to complete.
// The editor process inherits stdin, stdout, and stderr from the current process,
// allowing terminal editors to work correctly and GUI editors to display errors.
func OpenEditor(ctx context.Context, editor, path string) error {
	cmd := BuildEditorCommand(ctx, editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := procutil.RunWithCleanup(ctx, cmd); err != nil {
		return fmt.Errorf("running editor %q with %q: %w", editor, path, err)
	}

	return nil
}

// Edit opens the specified file in the user's preferred editor.
// It detects the editor using GetEditor, then launches it with OpenEditor.
func Edit(ctx context.Context, path string) error {
	editor := GetEditor(ctx)
	return OpenEditor(ctx, editor, path)
}
