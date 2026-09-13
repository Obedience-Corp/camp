package commit

import "context"

// InitOptions configures the first commit of a freshly scaffolded campaign.
type InitOptions struct {
	Options
	Description string // Body text listing what the scaffold produced
}

// Init commits the scaffold a campaign init just wrote. Files should be set to
// the scaffold's own paths so an init inside an existing repository never
// sweeps unrelated working-tree changes into the campaign's first commit.
func Init(ctx context.Context, opts InitOptions) Result {
	return doCommit(ctx, opts.Options, "Init", "scaffold camp workspace", opts.Description)
}
