package initcmd

import (
	"context"
	"io"
	"os"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/starter"
)

// EnsureStarter creates or registers the starter camp for a user with no
// camps. It is the one path shared by `camp setup` and by commands that
// create the starter on first use, so every entry point scaffolds the same
// workspace. errOut receives scaffold diagnostics.
func EnsureStarter(ctx context.Context, errOut io.Writer) (starter.Result, error) {
	// The starter is user-scoped, even when launched from an active camp shell.
	previous, wasSet := os.LookupEnv(campaign.EnvCampaignRoot)
	_ = os.Unsetenv(campaign.EnvCampaignRoot)
	defer func() {
		if wasSet {
			_ = os.Setenv(campaign.EnvCampaignRoot, previous)
		}
	}()
	return starter.Ensure(ctx, func(ctx context.Context, path string, resume bool) error {
		// A partial scaffold before campaign.yaml exists can safely resume with
		// repair: all writes are confined to our journaled staging directory.
		p := Params{Dir: path, Name: starter.Name, TypeStr: "personal", Description: "Your first Festival workspace", Mission: "Organize projects and turn ideas into completed work", NoRegister: true, Repair: resume, Yes: true}
		if err := RunFlow(ctx, p, Writers{HumanOut: io.Discard, ErrOut: errOut}, false); err != nil {
			return err
		}
		// Repair may return early when the scaffold is already complete. Ensure
		// fest initialization succeeded before publishing the workspace.
		return fest.RunInit(ctx, &fest.InitOptions{CampaignRoot: path})
	})
}
