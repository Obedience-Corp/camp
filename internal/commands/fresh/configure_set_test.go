package fresh

import (
	"context"
	"testing"
)

func TestApplyFreshSettingRefusesProjectPrune(t *testing.T) {
	_, err := applyFreshSetting(context.Background(), "", freshSetInput{
		Key:     "prune",
		Action:  "off",
		Project: "api",
	})
	if err == nil {
		t.Fatal("project prune write should fail")
	}
}

func TestApplyFreshSettingRejectsEmptyBranch(t *testing.T) {
	_, err := applyFreshSetting(context.Background(), "", freshSetInput{
		Key:    "branch",
		Action: "branch",
	})
	if err == nil {
		t.Fatal("empty --value should fail")
	}
}

func TestApplyFreshSettingRejectsUnknownKey(t *testing.T) {
	_, err := applyFreshSetting(context.Background(), "", freshSetInput{
		Key:    "merged_workitems",
		Action: "on",
	})
	if err == nil {
		t.Fatal("unknown key should fail")
	}
}
