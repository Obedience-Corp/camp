package flow

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestFlowAddUsesFromJSONFlag(t *testing.T) {
	cmd := newAddCommand(&cobra.Command{})

	if cmd.Flags().Lookup("from-json") == nil {
		t.Fatal("flow add missing --from-json flag")
	}
	if cmd.Flags().Lookup("json") != nil {
		t.Fatal("flow add still exposes --json input flag")
	}
	if cmd.Flags().ShorthandLookup("j") != nil {
		t.Fatal("flow add still exposes -j shorthand")
	}
}
