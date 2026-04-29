package crawl

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/intent"
)

// Preview limits per design 02-command-and-ux.md.
const (
	previewMaxLines = 8
	previewMaxChars = 1200
)

// PreviewHeader returns the multi-line metadata header shown above
// the body preview. Order matches the design example.
func PreviewHeader(in *intent.Intent) string {
	updated := updatedDate(in)
	promoted := dashIfEmpty(in.PromotedTo)

	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\n", in.ID)
	fmt.Fprintf(&b, "Status: %s | Type: %s | Priority: %s | Horizon: %s\n",
		stringOrDash(string(in.Status)),
		stringOrDash(string(in.Type)),
		stringOrDash(string(in.Priority)),
		stringOrDash(string(in.Horizon)),
	)
	fmt.Fprintf(&b, "Concept: %s | Updated: %s\n",
		stringOrDash(in.Concept),
		updated,
	)
	fmt.Fprintf(&b, "Promoted to: %s", promoted)
	return b.String()
}

// PreviewBody returns the trimmed body preview. It collapses long
// runs of blank lines to one, caps line count, and caps character
// count. Returns an empty string if there is no body content.
func PreviewBody(in *intent.Intent) string {
	body := strings.TrimSpace(in.Content)
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, previewMaxLines)
	prevBlank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		out = append(out, line)
		if len(out) >= previewMaxLines {
			break
		}
	}
	preview := strings.Join(out, "\n")
	if len(preview) > previewMaxChars {
		preview = preview[:previewMaxChars] + "..."
	}
	return preview
}

// PreviewDescription returns the full prompt Description text:
// header followed by a blank line and the body preview. It is what
// the runner passes to crawl.Item.Description.
func PreviewDescription(in *intent.Intent) string {
	header := PreviewHeader(in)
	body := PreviewBody(in)
	if body == "" {
		return header
	}
	return header + "\n\n" + body
}

// PreviewTitle returns the per-item heading line used by the prompt.
// Index is 1-based so the caller does not have to add 1.
func PreviewTitle(index, total int, in *intent.Intent) string {
	return fmt.Sprintf("Intent %d/%d: %s", index, total, stringOrDash(in.Title))
}

func updatedDate(in *intent.Intent) string {
	if !in.UpdatedAt.IsZero() {
		return in.UpdatedAt.Format("2006-01-02")
	}
	if !in.CreatedAt.IsZero() {
		return in.CreatedAt.Format("2006-01-02")
	}
	return "-"
}

func stringOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func dashIfEmpty(s string) string { return stringOrDash(s) }
