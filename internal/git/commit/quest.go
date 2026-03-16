package commit

import "context"

// QuestAction represents the type of quest operation performed.
type QuestAction string

const (
	QuestCreate   QuestAction = "QuestCreate"
	QuestRename   QuestAction = "QuestRename"
	QuestEdit     QuestAction = "QuestEdit"
	QuestPause    QuestAction = "QuestPause"
	QuestResume   QuestAction = "QuestResume"
	QuestComplete QuestAction = "QuestComplete"
	QuestArchive  QuestAction = "QuestArchive"
	QuestRestore  QuestAction = "QuestRestore"
	QuestLink     QuestAction = "QuestLink"
	QuestUnlink   QuestAction = "QuestUnlink"
)

// QuestOptions configures a quest commit.
type QuestOptions struct {
	Options
	Action    QuestAction
	QuestID   string
	QuestName string
	Detail    string
}

// Quest stages changes and commits for a quest operation.
func Quest(ctx context.Context, opts QuestOptions) Result {
	opts.Options.QuestID = opts.QuestID
	return doCommit(ctx, opts.Options, string(opts.Action), opts.QuestName, opts.Detail)
}
