package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunLeadingHelpFlag(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{Use: "run", Long: "run command help"}
			cmd.SetOut(&output)
			if err := runRun(cmd, []string{flag}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "run command help") {
				t.Fatalf("leading %s did not show help: %q", flag, output.String())
			}
		})
	}
}
