package feedback

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

// Scanner walks festival directories and collects feedback observations.
type Scanner struct {
	festivalsRoot string
}

// NewScanner creates a scanner for the given festivals root directory.
func NewScanner(festivalsRoot string) *Scanner {
	return &Scanner{festivalsRoot: festivalsRoot}
}

// Scan walks festival directories and returns all festivals with feedback observations.
func (s *Scanner) Scan(ctx context.Context, opts GatherOptions) ([]FestivalFeedback, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	statuses := opts.Statuses
	if len(statuses) == 0 {
		statuses = []string{"completed", "active", "planned"}
	}

	var results []FestivalFeedback

	for _, status := range statuses {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}

		statusDir := filepath.Join(s.festivalsRoot, status)
		entries, err := os.ReadDir(statusDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, camperrors.Wrapf(err, "reading %s directory", status)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			festDir := filepath.Join(statusDir, entry.Name())
			fb, err := s.scanFestival(ctx, festDir, status, opts)
			if err != nil {
				// Only warn for festivals that have feedback but malformed goals
				if hasFeedbackDir(festDir) {
					fmt.Fprintf(os.Stderr, "Warning: skipping festival %s: %v\n", entry.Name(), err)
				}
				continue
			}
			if fb == nil {
				continue // No feedback observations
			}

			results = append(results, *fb)
		}
	}

	return results, nil
}

// scanFestival reads a single festival directory for feedback observations.
func (s *Scanner) scanFestival(ctx context.Context, festDir, statusDir string, opts GatherOptions) (*FestivalFeedback, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	// Read FESTIVAL_GOAL.md frontmatter
	info, err := s.readGoalFrontmatter(festDir)
	if err != nil {
		return nil, err
	}
	info.Status = statusDir
	info.Path = festDir

	// Filter by festival ID if specified
	if opts.FestivalID != "" && info.ID != opts.FestivalID {
		return nil, nil
	}

	// Check for feedback/observations/ directory
	obsDir := filepath.Join(festDir, "feedback", "observations")
	entries, err := os.ReadDir(obsDir)
	if os.IsNotExist(err) {
		return nil, nil // No feedback directory
	}
	if err != nil {
		return nil, camperrors.Wrap(err, "reading observations")
	}

	var observations []Observation
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		obs, err := s.readObservation(filepath.Join(obsDir, entry.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping observation %s: %v\n", entry.Name(), err)
			continue
		}

		// Filter by severity if specified
		if opts.Severity != "" && !strings.EqualFold(obs.Severity, opts.Severity) {
			continue
		}

		observations = append(observations, *obs)
	}

	if len(observations) == 0 {
		return nil, nil
	}

	// Sort by ID
	sort.Slice(observations, func(i, j int) bool {
		return observations[i].ID < observations[j].ID
	})

	return &FestivalFeedback{
		Festival:     *info,
		Observations: observations,
	}, nil
}

// readGoalFrontmatter extracts frontmatter from FESTIVAL_GOAL.md.
func (s *Scanner) readGoalFrontmatter(festDir string) (*FestivalInfo, error) {
	goalPath := filepath.Join(festDir, "FESTIVAL_GOAL.md")
	data, err := os.ReadFile(goalPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "reading FESTIVAL_GOAL.md")
	}

	fm, err := parseFrontmatter(data)
	if err != nil {
		return nil, camperrors.Wrap(err, "parsing frontmatter")
	}

	if fm.ID == "" {
		return nil, fmt.Errorf("FESTIVAL_GOAL.md missing fest_id")
	}

	return &FestivalInfo{
		ID:   fm.ID,
		Name: fm.Name,
	}, nil
}

// readObservation parses a single observation YAML file.
func (s *Scanner) readObservation(path string) (*Observation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var obs Observation
	if err := yaml.Unmarshal(data, &obs); err != nil {
		return nil, camperrors.Wrap(err, "parsing observation")
	}

	return &obs, nil
}

// hasFeedbackDir checks if a festival directory contains a feedback/observations directory.
func hasFeedbackDir(festDir string) bool {
	info, err := os.Stat(filepath.Join(festDir, "feedback", "observations"))
	return err == nil && info.IsDir()
}

// parseFrontmatter extracts YAML frontmatter from a markdown document.
func parseFrontmatter(data []byte) (*GoalFrontmatter, error) {
	content := string(data)

	// Find frontmatter delimiters
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("no frontmatter found")
	}

	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}

	fmContent := content[4 : 4+end]

	var fm GoalFrontmatter
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		return nil, camperrors.Wrap(err, "parsing YAML")
	}

	return &fm, nil
}
