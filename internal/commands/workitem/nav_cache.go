package workitem

import (
	"fmt"

	"github.com/spf13/cobra"

	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
)

func invalidateNavigationCache(cmd *cobra.Command, campaignRoot string) {
	if err := navindex.Delete(campaignRoot); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to invalidate navigation cache: %v\n", err)
	}
}
