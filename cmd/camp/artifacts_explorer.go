package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func runArtifactsExplorer(cmd *cobra.Command, _ []string) error {
	root, err := campaign.DetectCached(cmd.Context())
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	plain, _ := cmd.Flags().GetBool("plain")
	if asJSON || plain || !ui.IsTerminal() {
		files, err := artifacts.Inventory(cmd.Context(), root)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				Version int                       `json:"version"`
				Files   []artifacts.InventoryFile `json:"files"`
			}{1, files})
		}
		if len(files) == 0 {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "No local artifact files. Declare a root with camp artifacts add <path>.")
			return err
		}
		for _, f := range files {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%9s  %s\n", ui.FormatBytes(f.Size), artifactDisplay(f.Path)); err != nil {
				return err
			}
		}
		return nil
	}
	pathOutput, _ := cmd.Flags().GetString("path-output")
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	m := newArtifactModel(ctx, root, pathOutput != "")
	final, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithAltScreen()).Run()
	if err != nil {
		return camperrors.Wrap(err, "running artifact explorer")
	}
	if done, ok := final.(artifactModel); ok && done.gotoPath != "" && pathOutput != "" {
		if err := os.WriteFile(pathOutput, []byte(done.gotoPath), 0600); err != nil {
			return camperrors.Wrap(err, "write artifact navigation path")
		}
	}
	return nil
}

func artifactAction(ctx context.Context, root, path, action string) tea.Cmd {
	return func() tea.Msg {
		abs := filepath.Join(root, filepath.FromSlash(path))
		result := artifactActionMsg{action: action, path: abs}
		// Refresh and actions can race a deletion. Report it instead of handing a
		// stale path to the opener or leaving the explorer for a missing directory.
		if _, err := os.Lstat(abs); err != nil {
			result.err = camperrors.Wrap(err, "artifact is unavailable")
			return result
		}
		switch action {
		case "go":
			result.path = filepath.Dir(abs)
		case "copy":
			result.err = ui.WriteClipboard(abs)
		case "open":
			var c *exec.Cmd
			switch runtime.GOOS {
			case "darwin":
				c = exec.CommandContext(ctx, "open", abs)
			case "linux", "freebsd":
				c = exec.CommandContext(ctx, "xdg-open", abs)
			default:
				result.err = camperrors.New("file opening is not supported on " + runtime.GOOS)
				return result
			}
			// This runs in a Tea command so a slow desktop opener never blocks keys.
			if output, err := c.CombinedOutput(); err != nil {
				result.err = camperrors.Wrapf(err, "open artifact: %s", strings.TrimSpace(string(output)))
			}
		}
		return result
	}
}
