package jsoncontract

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestDeclaredAsksTheCommandsOwnPredicate(t *testing.T) {
	declaring := &cobra.Command{Use: "feed"}
	silent := &cobra.Command{Use: "plain"}
	asJSON := false
	Declare(declaring, func() bool { return asJSON })

	if Declared(declaring) {
		t.Fatal("Declared = true while the command's predicate says false")
	}
	asJSON = true
	if !Declared(declaring) {
		t.Fatal("Declared = false while the command's predicate says true")
	}
	if Declared(silent) {
		t.Fatal("Declared = true for a command that declared nothing")
	}
}
