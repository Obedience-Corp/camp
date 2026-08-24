// Package tokens provides token counting for campaign work items using the
// tcount tokenizer. Counts are cached under .campaign/cache/ so repeated
// invocations do not recalculate when a file's content is unchanged.
package tokens

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/lancekrogers/tcount/tokenizer"

	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// DefaultModel is the tokenizer model used when no explicit model is given.
// gpt-4o uses exact o200k_base BPE tokenization and is a widely understood
// reference point for work-item magnitude.
const DefaultModel = "gpt-4o"

const (
	cacheDir  = ".campaign/cache"
	cacheFile = "tokens.json"
)

// Counter counts tokens for files using a tcount tokenizer, caching results
// by content digest so unchanged files are not recounted.
type Counter struct {
	model     string
	tokenizer *tokenizer.Counter
	cachePath string

	mu    sync.Mutex
	cache tokenCache
}

type tokenCache struct {
	Entries map[string]cacheEntry `json:"entries"`
}

type cacheEntry struct {
	Digest string `json:"digest"`
	Tokens int    `json:"tokens"`
	Model  string `json:"model"`
}

// NewCounter creates a token counter for the given model. The campaignRoot
// determines where the cache file lives (.campaign/cache/tokens.json).
func NewCounter(model, campaignRoot string) (*Counter, error) {
	if model == "" {
		model = DefaultModel
	}
	tc, err := tokenizer.NewCounter(tokenizer.CounterOptions{})
	if err != nil {
		return nil, err
	}
	c := &Counter{
		model:     model,
		tokenizer: tc,
		cachePath: filepath.Join(campaignRoot, cacheDir, cacheFile),
	}
	c.cache.Entries = map[string]cacheEntry{}
	c.loadCache()
	return c, nil
}

// CountFile returns the token count for a single file, using the cache when
// the file content digest matches a prior computation.
func (c *Counter) CountFile(ctx context.Context, path string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	digest := sha256.Sum256(content)
	key := cacheKey(path, c.model)

	c.mu.Lock()
	if entry, ok := c.cache.Entries[key]; ok && entry.Digest == hex.EncodeToString(digest[:]) && entry.Model == c.model {
		c.mu.Unlock()
		return entry.Tokens, nil
	}
	c.mu.Unlock()

	result, err := c.tokenizer.Count(ctx, string(content), c.model, false)
	if err != nil {
		return 0, err
	}
	tokens := 0
	for _, m := range result.Methods {
		tokens += m.Tokens
	}

	c.mu.Lock()
	c.cache.Entries[key] = cacheEntry{
		Digest: hex.EncodeToString(digest[:]),
		Tokens: tokens,
		Model:  c.model,
	}
	c.mu.Unlock()

	return tokens, nil
}

// SaveCache persists the in-memory cache to disk. A write failure is
// non-fatal: the count is still correct; only the cache optimization is lost.
func (c *Counter) SaveCache() error {
	c.mu.Lock()
	data, err := json.MarshalIndent(c.cache, "", "  ")
	c.mu.Unlock()
	if err != nil {
		return err
	}
	dir := filepath.Dir(c.cachePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomically(c.cachePath, data, 0o644)
}

func (c *Counter) loadCache() {
	data, err := os.ReadFile(c.cachePath)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &c.cache)
	if c.cache.Entries == nil {
		c.cache.Entries = map[string]cacheEntry{}
	}
}

func cacheKey(path, model string) string {
	return model + ":" + path
}
