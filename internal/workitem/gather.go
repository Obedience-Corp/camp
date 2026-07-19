package workitem

import (
	"context"
	"time"
)

// RecordGather stamps gathered_into and gathered_at on a gathered source: a
// directory workitem's .workitem marker or a file workitem's frontmatter.
// relPath is the source's campaign-relative location after the move into the
// gathered package. Existing keys are updated in place so the operation is
// idempotent.
func RecordGather(ctx context.Context, root, relPath, gatheredInto string, at time.Time) error {
	return recordLifecycleFields(ctx, root, relPath, []FrontmatterField{
		{After: "type", Key: "gathered_into", Value: gatheredInto},
		{After: "gathered_into", Key: "gathered_at", Value: at.UTC().Format(time.RFC3339)},
	})
}
