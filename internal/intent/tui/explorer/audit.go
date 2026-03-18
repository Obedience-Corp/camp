package explorer

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

func (m *Model) auditActor() string {
	actor := strings.TrimSpace(m.author)
	if actor == "" {
		return "system"
	}
	return actor
}

func (m *Model) appendAuditEvent(event audit.Event) error {
	if event.Actor == "" {
		event.Actor = m.auditActor()
	}
	return audit.AppendEvent(m.ctx, m.intentsDir, event)
}

func (m *Model) autoCommitFiles(files ...string) []string {
	if m.intentsDir != "" {
		files = append(files, audit.FilePath(m.intentsDir))
	}
	return commit.NormalizeFiles(m.campaignRoot, files...)
}

// autoCommitIntent performs a best-effort intent commit if campaign context is available.
func (m *Model) autoCommitIntent(action commit.IntentAction, title, description string, files ...string) {
	if m.campaignRoot == "" || m.campaignID == "" {
		return
	}
	_ = commit.Intent(m.ctx, commit.IntentOptions{
		Options: commit.Options{
			CampaignRoot:  m.campaignRoot,
			CampaignID:    m.campaignID,
			Files:         m.autoCommitFiles(files...),
			SelectiveOnly: true,
		},
		Action:      action,
		IntentTitle: title,
		Description: description,
	})
}
