package workitem

import (
	"errors"
	"strings"
	"testing"
)

func TestEvidenceDecision_SkipsUntaggedOnSubjectReadError(t *testing.T) {
	record, subj, warn := evidenceDecision("abc1234", "", errors.New("git log failed"))
	if record {
		t.Error("must not record ledger evidence when the subject read fails")
	}
	if subj != "" {
		t.Errorf("subject must be empty on read failure, got %q", subj)
	}
	if !strings.Contains(warn, "abc1234") || !strings.Contains(warn, "skipping ledger record") {
		t.Errorf("warning should name the sha and explain the skip: %q", warn)
	}
}

func TestEvidenceDecision_RecordsTaggedSubjectOnSuccess(t *testing.T) {
	const tagged = "feat: thing [camp:CA0001]"
	record, subj, warn := evidenceDecision("abc1234", tagged, nil)
	if !record {
		t.Error("should record evidence when the subject read succeeds")
	}
	if subj != tagged {
		t.Errorf("subject = %q, want the tagged subject %q", subj, tagged)
	}
	if warn != "" {
		t.Errorf("no warning expected on success, got %q", warn)
	}
}
