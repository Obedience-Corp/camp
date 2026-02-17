package commit

import "context"

// ProjectAction represents the type of project operation performed.
type ProjectAction string

const (
	ProjectAdd    ProjectAction = "Add"
	ProjectNew    ProjectAction = "New"
	ProjectRemove ProjectAction = "Remove"
)

// ProjectOptions configures a project commit.
type ProjectOptions struct {
	Options
	Action      ProjectAction // The action performed
	ProjectName string        // Name of the affected project
	Description string        // Optional body text
}

// Project stages all changes and commits for a project operation.
func Project(ctx context.Context, opts ProjectOptions) Result {
	return doCommit(ctx, opts.Options, string(opts.Action), opts.ProjectName, opts.Description)
}
