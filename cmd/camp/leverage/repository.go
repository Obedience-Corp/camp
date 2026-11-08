package leverage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/spf13/cobra"
)

func runRepositoryReport(cmd *cobra.Command, target, root string) error {
	ctx := cmd.Context()
	if target != "" {
		info, err := os.Stat(target)
		if err != nil {
			return camperrors.Wrap(err, "directory not found")
		}
		if !info.IsDir() {
			return camperrors.New("target is not a directory")
		}
		// Target settings never come from CAMP_ROOT or the caller's unrelated camp.
		for dir := target; ; dir = filepath.Dir(dir) {
			if info, err := os.Stat(filepath.Join(dir, ".campaign")); err == nil && info.IsDir() {
				root = dir
				break
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	cfg := &intleverage.LeverageConfig{COCOMOProjectType: "organic"}
	var authors *intleverage.AuthorConfig
	var err error
	if root != "" {
		cfg, err = intleverage.LoadConfig(intleverage.DefaultConfigPath(root))
		if err != nil {
			return err
		}
		authors, err = intleverage.LoadAuthorConfig(intleverage.DefaultAuthorsPath(root))
		if err != nil {
			return err
		}
	}
	if authors != nil {
		owners := map[string]string{}
		for id, a := range authors.Authors {
			for _, email := range a.Emails {
				key := strings.ToLower(strings.TrimSpace(email))
				if prior, ok := owners[key]; ok && prior != id {
					return camperrors.Newf("author email %s is assigned to both %s and %s", email, prior, id)
				}
				owners[key] = id
			}
		}
	}
	resolver := intleverage.NewAuthorResolver(authors)
	projects := []intleverage.ResolvedProject{}
	if target != "" {
		projects = append(projects, intleverage.ResolvedProject{Name: filepath.Base(target), SCCDir: target})
	} else {
		projects, err = intleverage.ResolveProjects(ctx, root, cfg)
		if err != nil {
			return err
		}
		filter, _ := cmd.Flags().GetString("project")
		projects, err = intleverage.FilterByName(projects, filter)
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			return camperrors.Newf("no projects match %q", filter)
		}
	}
	runner, err := intleverage.NewSCCRunner(cfg.COCOMOProjectType)
	if err != nil {
		return err
	}
	threshold, _ := cmd.Flags().GetFloat64("author-threshold")
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Analyzing repository source and ownership...")
	report, err := intleverage.AnalyzeRepositories(ctx, projects, runner, resolver, threshold)
	if err != nil {
		return err
	}
	report.Tools["camp"] = version.Version
	if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
		for _, p := range report.Projects {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Target: %s\nScope: %s (%d files)\nContent: %s\n", p.Target, p.ManifestDigest, p.Files, p.ContentDigest)
			for _, source := range p.ConfigSources {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Config: %s (%s)\n", source.Path, source.SHA256)
			}
		}
	}
	byAuthor, _ := cmd.Flags().GetBool("by-author")
	jsonOut, _ := cmd.Flags().GetBool("json")
	author, _ := cmd.Flags().GetString("author")
	people, _ := cmd.Flags().GetInt("people")
	var output any = report
	var score intleverage.RepositoryScore
	if !byAuthor {
		var matched []string
		var method string
		score, matched, method, err = report.Score(author, people, resolver)
		if err != nil {
			return err
		}
		view := "summary"
		if author != "" {
			view = "personal"
		}
		output = map[string]any{"schema_version": intleverage.RepositoryReportSchema, "view": view, "status": report.Status, "campaign": score, "projects": []intleverage.RepositoryScore{score}, "report": map[string]any{
			"generated_at": report.GeneratedAt, "tools": report.Tools, "history_method": report.History, "summary": report.Summary, "diagnostics": report.Diagnostics, "scope": report.Projects[0], "effort_method": method, "people_override": people, "author_selector": author, "matched_author_ids": matched,
		}}
	}
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(output); err != nil {
			return err
		}
	} else {
		if err := renderRepositoryReport(cmd, report, byAuthor, author, score); err != nil {
			return err
		}
	}
	for _, d := range report.Diagnostics {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s (%s)\n", d.Code, d.Message, d.Path)
	}
	if report.Status == "partial" {
		return camperrors.NewCommand(cmd.CommandPath(), 2, "", nil)
	}
	return nil
}

func renderRepositoryReport(cmd *cobra.Command, r *intleverage.RepositoryReport, byAuthor bool, author string, score intleverage.RepositoryScore) error {
	var buffer strings.Builder
	out := &buffer
	_, _ = fmt.Fprintf(out, "Directory Leverage (%s) [%s]\n", repositoryLabel(r), r.Status)
	for _, p := range r.Projects {
		_, _ = fmt.Fprintf(out, "  %s  HEAD %s  dirty=%t\n", p.Target, p.Head, p.Dirty)
	}
	_, _ = fmt.Fprintf(out, "COCOMO Estimate: %.2f person-months | %d files | %d code lines\n", r.Summary.EstimatedPM, r.Summary.Files, r.Summary.CodeLines)
	if byAuthor {
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "AUTHOR\tID\tLINES\tSHARE\tEST PM\tSPAN PM\tLEVERAGE")
		for _, a := range r.Contributors {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%.2f%%\t%.2f\t%s\t%s\n", a.Name, a.ID, a.Lines, a.Share*100, a.EstimatedPM, optionalNumber(a.ElapsedPM, ""), optionalNumber(a.Leverage, "x"))
		}
		_ = w.Flush()
		_, _ = fmt.Fprintf(out, "Omitted below %.2f%%: %d contributors (%.2f%% of lines)\n", r.Display.Threshold, r.Display.Omitted, r.Display.OmittedShare*100)
	} else {
		if author != "" {
			_, _ = fmt.Fprintf(out, "Author selection: %s\n", author)
		}
		_, _ = fmt.Fprintf(out, "Leverage: %s | Effort proxy: %s person-months | Historical authors: %d\n", optionalNumber(score.FullLeverage, "x"), optionalNumber(score.ActualPM, ""), score.AuthorCount)
	}
	for _, key := range []string{"contributors", "excluded_identities", "uncommitted", "unavailable"} {
		b := r.Summary.Buckets[key]
		_, _ = fmt.Fprintf(out, "%s: %d lines (%.2f%%), %.2f estimated PM\n", key, b.Lines, b.Share*100, b.EstimatedPM)
	}
	noLegend, _ := cmd.Flags().GetBool("no-legend")
	if !noLegend {
		_, _ = fmt.Fprintln(out, "Effort uses repository-wide local-ref commit spans, floored at 0.1 months.")
		_, _ = fmt.Fprintln(out, "Ownership counts surviving physical lines. Ignore rules limit ownership, not historical spans.")
		_, _ = fmt.Fprintln(out, "These estimates do not measure hours worked, AI use, or individual effectiveness.")
	}
	_, err := io.WriteString(cmd.OutOrStdout(), buffer.String())
	return err
}

func optionalNumber(n *float64, suffix string) string {
	if n == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f%s", *n, suffix)
}

func repositoryLabel(r *intleverage.RepositoryReport) string {
	if len(r.Projects) == 1 {
		return filepath.Base(r.Projects[0].Target)
	}
	return fmt.Sprintf("%d projects", len(r.Projects))
}
