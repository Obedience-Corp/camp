package leverage

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

var leverageConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "View or update leverage configuration",
	Long: `View or update leverage score configuration settings.

Without flags, displays the current configuration. With flags, updates
the configuration and saves it to .campaign/leverage/config.json.

Configuration parameters:
  --people       Number of developers on the team
  --start        Project start date (YYYY-MM-DD format)
  --cocomo-type  COCOMO project type (organic, semi-detached, embedded)
  --exclude      Exclude a project from leverage scoring
  --include      Include a previously excluded project
  --autocommit   Commit .campaign/leverage data automatically (default true)

Examples:
  camp leverage config                         Show current config
  camp leverage config --people 3              Set team size to 3
  camp leverage config --start 2025-01-01      Set project start date
  camp leverage config --exclude obey-daemon   Exclude a project
  camp leverage config --include obey-daemon   Re-include a project
  camp leverage config --autocommit=false      Stop auto-committing leverage data`,
	RunE: runLeverageConfig,
}

func init() {
	leverageConfigCmd.Flags().Int("people", 0, "number of developers on the team (0 = auto-detect from git)")
	leverageConfigCmd.Flags().String("start", "", "project start date (YYYY-MM-DD)")
	leverageConfigCmd.Flags().String("cocomo-type", "", "COCOMO project type (organic, semi-detached, embedded)")
	leverageConfigCmd.Flags().String("exclude", "", "exclude a project from leverage scoring")
	leverageConfigCmd.Flags().String("include", "", "include a previously excluded project")
	leverageConfigCmd.Flags().String("author-email", "", "default author email for personal leverage (empty = team view)")
	leverageConfigCmd.Flags().Bool("autocommit", true, "commit "+intleverage.DataDir+" data automatically after leverage commands")
	addNoCommitFlag(leverageConfigCmd)
	Cmd.AddCommand(leverageConfigCmd)
}

// configFlagChanges records which settings this invocation asked to change.
type configFlagChanges struct {
	people      bool
	start       bool
	cocomo      bool
	authorEmail bool
	autocommit  bool
	exclude     bool
	include     bool
}

func (c configFlagChanges) anySetting() bool {
	return c.people || c.start || c.cocomo || c.authorEmail || c.autocommit
}

func (c configFlagChanges) anyInclusion() bool {
	return c.exclude || c.include
}

func changedConfigFlags(cmd *cobra.Command) configFlagChanges {
	changed := func(name string) bool {
		flag := cmd.Flags().Lookup(name)
		return flag != nil && flag.Changed
	}
	return configFlagChanges{
		people:      changed("people"),
		start:       changed("start"),
		cocomo:      changed("cocomo-type"),
		authorEmail: changed("author-email"),
		autocommit:  changed("autocommit"),
		exclude:     changed("exclude"),
		include:     changed("include"),
	}
}

func runLeverageConfig(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}

	configPath := intleverage.DefaultConfigPath(root)
	changes := changedConfigFlags(cmd)

	switch {
	case changes.anyInclusion():
		return updateProjectInclusion(cmd, ctx, root, configPath, changes)
	case changes.anySetting():
		return updateLeverageConfig(cmd, ctx, root, configPath, changes)
	default:
		return displayLeverageConfig(cmd, ctx, root, configPath)
	}
}

func displayLeverageConfig(cmd *cobra.Command, ctx context.Context, root, configPath string) error {
	out := cmd.OutOrStdout()

	_, statErr := os.Stat(configPath)
	configExists := statErr == nil

	var cfg *intleverage.LeverageConfig

	if configExists {
		var err error
		cfg, err = intleverage.LoadConfig(configPath)
		if err != nil {
			return camperrors.Wrap(err, "loading config")
		}

		fmt.Fprintln(out, "Configuration: saved (.campaign/leverage/config.json)")
		fmt.Fprintln(out)
		if cfg.ActualPeople == 0 {
			fmt.Fprintln(out, "Team Size:     auto-detect from git")
		} else {
			fmt.Fprintf(out, "Team Size:     %d developer(s) (override)\n", cfg.ActualPeople)
		}
		fmt.Fprintf(out, "Project Start: %s\n", cfg.ProjectStart.Format("2006-01-02"))
		fmt.Fprintf(out, "COCOMO Type:   %s\n", cfg.COCOMOProjectType)
		if cfg.AvgWage > 0 {
			fmt.Fprintf(out, "Avg Wage:      $%.0f/year\n", cfg.AvgWage)
		}
		if cfg.AuthorEmail != "" {
			fmt.Fprintf(out, "Author Email:  %s (default --author)\n", cfg.AuthorEmail)
		}
		_, _ = fmt.Fprintf(out, "Autocommit:    %t\n", cfg.AutocommitEnabled())
	} else {
		var err error
		cfg, err = intleverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return camperrors.Wrap(err, "auto-detecting config")
		}

		fmt.Fprintln(out, "Configuration: auto-detected (no config file found)")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Team Size:     auto-detect from git")
		if !cfg.ProjectStart.IsZero() {
			fmt.Fprintf(out, "Project Start: %s (earliest git commit)\n", cfg.ProjectStart.Format("2006-01-02"))
		} else {
			fmt.Fprintln(out, "Project Start: unknown (no git history found)")
		}
		fmt.Fprintf(out, "COCOMO Type:   %s\n", cfg.COCOMOProjectType)
	}

	resolved, _ := intleverage.ResolveProjects(ctx, root, cfg)
	authResolver := intleverage.NewAuthorResolver(nil)
	if authCfg, loadErr := intleverage.LoadAuthorConfig(intleverage.DefaultAuthorsPath(root)); loadErr == nil && authCfg != nil {
		authResolver = intleverage.NewAuthorResolver(authCfg)
	}
	authorCounts := make(map[string]int)
	for _, proj := range resolved {
		count, err := intleverage.CountAuthors(ctx, proj.GitDir, authResolver)
		if err == nil {
			authorCounts[proj.Name] = count
		}
	}

	if len(cfg.Projects) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Projects:")
		names := make([]string, 0, len(cfg.Projects))
		for name := range cfg.Projects {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			entry := cfg.Projects[name]
			status := "included"
			if !entry.Include {
				status = "excluded"
			}
			authorInfo := ""
			if count, ok := authorCounts[name]; ok && entry.Include {
				authorInfo = fmt.Sprintf("  (%d %s)", count, pluralize(count, "author", "authors"))
			}
			fmt.Fprintf(out, "  %-20s %s%s\n", name, status, authorInfo)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Config path:   %s\n", configPath)
	fmt.Fprintln(out, "\nTo update: camp leverage config --people N --start YYYY-MM-DD")

	return nil
}

func updateProjectInclusion(cmd *cobra.Command, ctx context.Context, root, configPath string, changes configFlagChanges) error {
	out := cmd.OutOrStdout()

	cfg, err := intleverage.LoadConfig(configPath)
	if err != nil {
		return camperrors.Wrap(err, "loading config")
	}

	if len(cfg.Projects) == 0 {
		if err := intleverage.PopulateProjects(ctx, root, cfg); err != nil {
			return camperrors.Wrap(err, "populating projects")
		}
	}

	var applied []string
	if changes.exclude {
		name, _ := cmd.Flags().GetString("exclude")
		if err := setProjectInclusion(cfg, name, false); err != nil {
			return err
		}
		applied = append(applied, "Excluded project: "+name)
	}

	if changes.include {
		name, _ := cmd.Flags().GetString("include")
		if err := setProjectInclusion(cfg, name, true); err != nil {
			return err
		}
		applied = append(applied, "Included project: "+name)
	}

	if err := intleverage.SaveConfig(configPath, cfg); err != nil {
		return camperrors.Wrap(err, "saving config")
	}

	for _, line := range applied {
		_, _ = fmt.Fprintln(out, line)
	}
	fmt.Fprintf(out, "Saved to: %s\n", configPath)

	autocommitLeverageData(ctx, cmd, out, autocommitInput{
		root:    root,
		cfg:     cfg,
		subject: "config",
		report:  renderConfigReport(applied),
	})
	return nil
}

func setProjectInclusion(cfg *intleverage.LeverageConfig, name string, include bool) error {
	entry, exists := cfg.Projects[name]
	if !exists {
		return camperrors.Newf("project %q not found in config", name)
	}
	entry.Include = include
	cfg.Projects[name] = entry
	return nil
}

func updateLeverageConfig(cmd *cobra.Command, ctx context.Context, root, configPath string, changes configFlagChanges) error {
	out := cmd.OutOrStdout()

	cfg, err := intleverage.LoadConfig(configPath)
	if err != nil {
		return camperrors.Wrap(err, "loading config")
	}

	applied, err := applyConfigChanges(cmd, cfg, changes)
	if err != nil {
		return err
	}

	if err := intleverage.SaveConfig(configPath, cfg); err != nil {
		return camperrors.Wrap(err, "saving config")
	}

	_, _ = fmt.Fprintln(out, "Configuration updated successfully")
	_, _ = fmt.Fprintf(out, "Saved to: %s\n", configPath)
	_, _ = fmt.Fprintln(out)
	printConfigSummary(out, cfg)

	autocommitLeverageData(ctx, cmd, out, autocommitInput{
		root:               root,
		cfg:                cfg,
		subject:            "config",
		report:             renderConfigReport(applied),
		ignoreConfigOptOut: changes.autocommit,
	})
	return nil
}

func applyConfigChanges(cmd *cobra.Command, cfg *intleverage.LeverageConfig, changes configFlagChanges) ([]string, error) {
	var applied []string

	if changes.people {
		people, _ := cmd.Flags().GetInt("people")
		if people < 0 {
			return nil, camperrors.Newf("people must be >= 0 (0 = auto-detect from git)")
		}
		cfg.ActualPeople = people
		applied = append(applied, fmt.Sprintf("Team size: %d", people))
	}

	if changes.authorEmail {
		email, _ := cmd.Flags().GetString("author-email")
		cfg.AuthorEmail = email
		applied = append(applied, "Author email: "+orNone(email))
	}

	if changes.start {
		startStr, _ := cmd.Flags().GetString("start")
		startDate, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			return nil, camperrors.Wrap(err, "invalid date format, use YYYY-MM-DD")
		}
		cfg.ProjectStart = startDate
		applied = append(applied, "Project start: "+startDate.Format("2006-01-02"))
	}

	if changes.cocomo {
		cocomoType, _ := cmd.Flags().GetString("cocomo-type")
		valid := map[string]bool{"organic": true, "semi-detached": true, "embedded": true}
		if !valid[cocomoType] {
			return nil, camperrors.Newf("invalid COCOMO type %q: must be organic, semi-detached, or embedded", cocomoType)
		}
		cfg.COCOMOProjectType = cocomoType
		applied = append(applied, "COCOMO type: "+cocomoType)
	}

	if changes.autocommit {
		enabled, _ := cmd.Flags().GetBool("autocommit")
		cfg.Autocommit = &enabled
		applied = append(applied, fmt.Sprintf("Autocommit: %t", enabled))
	}

	return applied, nil
}

func printConfigSummary(out io.Writer, cfg *intleverage.LeverageConfig) {
	if cfg.ActualPeople == 0 {
		fmt.Fprintln(out, "Team Size:     auto-detect from git")
	} else {
		fmt.Fprintf(out, "Team Size:     %d developer(s)\n", cfg.ActualPeople)
	}
	fmt.Fprintf(out, "Project Start: %s\n", cfg.ProjectStart.Format("2006-01-02"))
	fmt.Fprintf(out, "COCOMO Type:   %s\n", cfg.COCOMOProjectType)
	if cfg.AuthorEmail != "" {
		fmt.Fprintf(out, "Author Email:  %s\n", cfg.AuthorEmail)
	}
	_, _ = fmt.Fprintf(out, "Autocommit:    %t\n", cfg.AutocommitEnabled())
}

func renderConfigReport(applied []string) string {
	if len(applied) == 0 {
		return "Updated leverage configuration."
	}
	return "Updated leverage configuration:\n\n" + strings.Join(applied, "\n")
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
