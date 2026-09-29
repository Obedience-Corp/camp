package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	initcmd "github.com/Obedience-Corp/camp/cmd/camp/init"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/starter"
	"github.com/spf13/cobra"
)

func init() {
	var asJSON bool
	cmd := &cobra.Command{
		Use: "setup", Short: "Create your first festival camp once",
		Long: "Create and register a camp named festival in your configured camps directory when you have no registered camps. Existing camps are preserved. Once setup succeeds, deleting the starter does not cause it to be recreated. Failed setup can be retried with the same command.",
		Args: cobra.NoArgs, GroupID: "setup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Setup is user-scoped, even when launched from an active camp shell.
			previous, wasSet := os.LookupEnv("CAMP_ROOT")
			_ = os.Unsetenv("CAMP_ROOT")
			defer func() {
				if wasSet {
					_ = os.Setenv("CAMP_ROOT", previous)
				}
			}()
			result, err := starter.Ensure(cmd.Context(), func(ctx context.Context, path string, resume bool) error {
				// A partial scaffold before campaign.yaml exists can safely resume with
				// repair: all writes are confined to our journaled staging directory.
				p := initcmd.Params{Dir: path, Name: starter.Name, TypeStr: "personal", Description: "Your first Festival workspace", Mission: "Organize projects and turn ideas into completed work", NoRegister: true, Repair: resume, Yes: true}
				if err := initcmd.RunFlow(ctx, p, initcmd.Writers{HumanOut: io.Discard, ErrOut: cmd.ErrOrStderr()}, false); err != nil {
					return err
				}
				// Repair may return early when the scaffold is already complete. Ensure
				// fest initialization succeeded before publishing the workspace.
				return fest.RunInit(ctx, &fest.InitOptions{CampaignRoot: path})
			})
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			}
			return renderStarterResult(cmd.OutOrStdout(), result)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the setup result as JSON")
	rootCmd.AddCommand(cmd)
}

func renderStarterResult(out io.Writer, result starter.Result) error {
	var err error
	switch result.Action {
	case "created", "registered":
		_, err = fmt.Fprintf(out, "Your festival camp is ready at %s\nEnter it with: cd %s\n", result.Path, "'"+strings.ReplaceAll(result.Path, "'", "'\"'\"'")+"'")
	case "existing":
		_, err = fmt.Fprintln(out, "Your existing camps are ready; no starter was added.")
	case "complete":
		_, err = fmt.Fprintln(out, "Starter camp setup has already completed.")
	}
	if err == nil && result.Message != "" {
		_, err = fmt.Fprintln(out, result.Message)
	}
	return err
}
