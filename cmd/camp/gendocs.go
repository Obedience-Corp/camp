package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Obedience-Corp/camp/internal/commands/release"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var (
	gendocsOutput string
	gendocsFormat string
	gendocsSingle bool
	gendocsStable bool
)

var gendocsCmd = &cobra.Command{
	Use:    "gendocs",
	Short:  "Generate CLI reference documentation",
	Long:   "Generate markdown or YAML reference documentation for all camp commands.",
	Hidden: true,
	RunE:   runGendocs,
}

func init() {
	gendocsCmd.Flags().StringVar(&gendocsOutput, "output", "docs", "output directory")
	gendocsCmd.Flags().StringVar(&gendocsFormat, "format", "markdown", "output format: markdown or yaml")
	gendocsCmd.Flags().BoolVar(&gendocsSingle, "single", false, "generate a single combined reference file")
	gendocsCmd.Flags().BoolVar(&gendocsStable, "stable", false, "exclude dev-only commands from generated docs")
	rootCmd.AddCommand(gendocsCmd)
}

func runGendocs(cmd *cobra.Command, args []string) error {
	if err := os.MkdirAll(gendocsOutput, 0755); err != nil {
		return camperrors.Wrap(err, "create output dir")
	}

	// Remove previously generated command docs before regenerating so switching
	// formats or profiles does not leave stale generated files behind.
	if err := removeGeneratedDocs(gendocsOutput, "camp"); err != nil {
		return camperrors.Wrap(err, "clean generated docs")
	}

	rootForDocs := rootCmd
	if cmd != nil {
		rootForDocs = cmd.Root()
	}

	stripANSIFromTree(rootForDocs)
	disableAutoGenTag(rootForDocs)

	if gendocsStable {
		hideDevOnlyCommands(rootForDocs)
	}

	switch gendocsFormat {
	case "markdown":
		if err := doc.GenMarkdownTree(rootForDocs, gendocsOutput); err != nil {
			return camperrors.Wrap(err, "generate markdown")
		}
		fmt.Fprintf(os.Stderr, "Markdown docs written to %s\n", gendocsOutput)
	case "yaml":
		if err := doc.GenYamlTree(rootForDocs, gendocsOutput); err != nil {
			return camperrors.Wrap(err, "generate yaml")
		}
		fmt.Fprintf(os.Stderr, "YAML docs written to %s\n", gendocsOutput)
	default:
		return fmt.Errorf("unknown format %q (use markdown or yaml)", gendocsFormat)
	}

	if gendocsSingle && gendocsFormat == "markdown" {
		if err := combineSingleFile(gendocsOutput, "camp"); err != nil {
			return camperrors.Wrap(err, "generate single file")
		}
	}

	return nil
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func stripANSIFromTree(cmd *cobra.Command) {
	cmd.Short = stripANSI(cmd.Short)
	cmd.Long = stripANSI(cmd.Long)
	cmd.Example = stripANSI(cmd.Example)

	for _, g := range cmd.Groups() {
		g.Title = stripANSI(g.Title)
	}

	for _, child := range cmd.Commands() {
		stripANSIFromTree(child)
	}
}

func disableAutoGenTag(cmd *cobra.Command) {
	cmd.DisableAutoGenTag = true
	for _, child := range cmd.Commands() {
		disableAutoGenTag(child)
	}
}

// hideDevOnlyCommands removes commands annotated as dev-only from the tree.
// Cobra's doc generators only skip Hidden commands, not annotated ones,
// so we remove dev-only commands entirely before generating stable docs.
func hideDevOnlyCommands(parent *cobra.Command) {
	for _, child := range parent.Commands() {
		if child.Annotations[release.AnnotationReleaseChannel] == release.ReleaseChannelDevOnly {
			parent.RemoveCommand(child)
		} else {
			hideDevOnlyCommands(child)
		}
	}
}

func removeGeneratedDocs(dir, name string) error {
	patterns := []string{
		filepath.Join(dir, name+"_*.md"),
		filepath.Join(dir, name+"_*.yaml"),
		filepath.Join(dir, name+"-reference.md"),
	}

	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}
		for _, file := range files {
			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return nil
}

// stripSeeAlso removes the "### SEE ALSO" section from markdown content.
// Used when combining individual command docs into a single reference page
// where cross-links are redundant.
func stripSeeAlso(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	skip := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "### SEE ALSO" {
			skip = true
			// Also remove any blank line immediately before the heading
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				out = out[:len(out)-1]
			}
			continue
		}
		if skip {
			// Stop skipping at next heading or horizontal rule (section boundary)
			if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "---" {
				skip = false
				out = append(out, line)
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func combineSingleFile(dir, name string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("---\ntitle: \"%s CLI Reference\"\nweight: 1\n---\n\n# %s CLI Reference\n", name, name))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Name() == name+"-reference.md" || entry.Name() == "_index.md" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		parts = append(parts, stripSeeAlso(string(data)))
	}

	combined := strings.Join(parts, "\n---\n\n")
	outPath := filepath.Join(dir, name+"-reference.md")
	if err := os.WriteFile(outPath, []byte(combined), 0644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Combined reference written to %s\n", outPath)
	return nil
}
