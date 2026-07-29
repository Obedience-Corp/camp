package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/obey-shared/festivalbundle"
	"github.com/spf13/cobra"
)

// UnbundleCmd materializes a .festival into a directory.
var UnbundleCmd = &cobra.Command{
	Use:     "unbundle",
	Short:   "Unbundle a .festival archive into a directory",
	Long:    "Extract a Festival Bundle into a live work-unit directory. Does not execute festivals or rituals.",
	GroupID: "planning",
	Args:    cobra.ExactArgs(1),
	RunE:    runUnbundle,
}

var (
	unbundleDest     string
	unbundleForce    bool
	unbundleNoVerify bool
	unbundleNoRecv   bool
	unbundleJSON     bool
)

func init() {
	UnbundleCmd.Flags().StringVarP(&unbundleDest, "dest", "d", "", "destination directory (required)")
	UnbundleCmd.Flags().BoolVar(&unbundleForce, "force", false, "allow non-empty destination")
	UnbundleCmd.Flags().BoolVar(&unbundleNoVerify, "no-verify", false, "skip bundle.id content-hash verification")
	UnbundleCmd.Flags().BoolVar(&unbundleNoRecv, "no-received-record", false, "do not write .bundles/received")
	UnbundleCmd.Flags().BoolVar(&unbundleJSON, "json", false, "emit info.json as JSON on stdout")
	_ = UnbundleCmd.MarkFlagRequired("dest")
}

func runUnbundle(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	festivalPath, err := filepath.Abs(args[0])
	if err != nil {
		return camperrors.NewCommand(cmd.CommandPath(), 1, "resolve festival", err)
	}
	if st, err := os.Stat(festivalPath); err != nil || st.IsDir() {
		return camperrors.NewCommand(cmd.CommandPath(), 1, "festival", fmt.Errorf("not a file: %s", festivalPath))
	}
	dest, err := filepath.Abs(unbundleDest)
	if err != nil {
		return camperrors.NewCommand(cmd.CommandPath(), 1, "dest", err)
	}

	info, err := festivalbundle.Unbundle(ctx, festivalPath, dest, festivalbundle.UnbundleOptions{
		Force:               unbundleForce,
		SkipVerify:          unbundleNoVerify,
		WriteReceivedRecord: !unbundleNoRecv,
	})
	if err != nil {
		return camperrors.NewCommand(cmd.CommandPath(), 1, "unbundle", err)
	}

	if unbundleJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "unbundled to %s\n", dest)
	fmt.Fprintf(cmd.OutOrStdout(), "kind=%s id=%s name=%q\n", info.Kind, info.Bundle.ID, info.Bundle.Name)
	return nil
}
