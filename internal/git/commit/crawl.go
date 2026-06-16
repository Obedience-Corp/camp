package commit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const crawlIDLen = 3

func NewCrawlID() (string, error) {
	b := make([]byte, crawlIDLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CrawlOptions configures a dungeon crawl commit.
type CrawlOptions struct {
	Options
	CrawlID     string   // Unique identifier for this crawl session (e.g. "a3f1b2")
	Description string   // Body text describing moved items
	Files       []string // Paths to stage (e.g., parent dir + dungeon dir)
}

// Crawl stages changes and commits for a dungeon crawl operation.
// If Files is set, only those paths are staged instead of everything.
// SelectiveOnly is automatically enabled when Files or PreStaged is provided.
func Crawl(ctx context.Context, opts CrawlOptions) Result {
	opts.Options.Files = opts.Files
	if len(opts.Files) > 0 || len(opts.PreStaged) > 0 {
		opts.SelectiveOnly = true
	}
	subject := crawlSubject(opts.CrawlID)
	return doCommit(ctx, opts.Options, "Crawl", subject, opts.Description)
}

func crawlSubject(crawlID string) string {
	if crawlID == "" {
		return "dungeon crawl completed"
	}
	return fmt.Sprintf("dungeon crawl completed [CW-%s]", crawlID)
}

func IntentCrawlSubject(crawlID string) string {
	if crawlID == "" {
		return "intent crawl completed"
	}
	return fmt.Sprintf("intent crawl completed [CW-%s]", crawlID)
}
