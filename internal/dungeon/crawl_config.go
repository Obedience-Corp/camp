package dungeon

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// CrawlConfigFile is the name of the crawl configuration file stored in dungeon directories.
const CrawlConfigFile = ".crawl.yaml"

// CrawlConfig holds crawl-specific configuration for a dungeon directory.
// It lives alongside crawl.jsonl in the dungeon root.
type CrawlConfig struct {
	// Excludes lists directory names in the parent that should be excluded
	// from triage crawl. These are structural/resource directories that
	// should never be presented as candidates for moving into the dungeon.
	Excludes []string `yaml:"excludes"`
}

// loadCrawlConfig loads a .crawl.yaml file from the given path.
// Returns nil config and an error if the file doesn't exist or can't be parsed.
func loadCrawlConfig(path string) (*CrawlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading crawl config: %w", err)
	}

	var cfg CrawlConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing crawl config: %w", err)
	}

	return &cfg, nil
}
