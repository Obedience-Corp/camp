package quest

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var (
	questWhitespacePattern = regexp.MustCompile(`\s+`)
	questNonAlphanumeric   = regexp.MustCompile(`[^a-z0-9-]+`)
	questMultipleHyphens   = regexp.MustCompile(`-+`)
)

const questAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// GenerateSlug creates a filesystem-safe slug from the quest name.
func GenerateSlug(name string) string {
	slug := strings.ToLower(name)
	slug = questWhitespacePattern.ReplaceAllString(slug, "-")
	slug = questNonAlphanumeric.ReplaceAllString(slug, "")
	slug = questMultipleHyphens.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return ""
	}

	words := strings.Split(slug, "-")
	if len(words) > 5 {
		words = words[:5]
	}
	slug = strings.Join(words, "-")

	if len(slug) > 50 {
		slug = strings.TrimRight(slug[:50], "-")
	}

	return slug
}

// GenerateDirectorySlug creates the immutable quest directory slug.
func GenerateDirectorySlug(name string, ts time.Time) string {
	datePrefix := ts.Format("20060102")
	slug := GenerateSlug(name)
	if slug == "" {
		return datePrefix
	}
	return fmt.Sprintf("%s-%s", datePrefix, slug)
}

// GenerateID creates a quest ID in qst_YYYYMMDD_xxxxxx format.
func GenerateID(ts time.Time) (string, error) {
	suffix, err := randomString(6)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("qst_%s_%s", ts.Format("20060102"), suffix), nil
}

func randomString(n int) (string, error) {
	if n <= 0 {
		return "", nil
	}
	var b strings.Builder
	b.Grow(n)
	limit := big.NewInt(int64(len(questAlphabet)))
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		b.WriteByte(questAlphabet[idx.Int64()])
	}
	return b.String(), nil
}
