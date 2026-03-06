//go:build !dev

package main

import "testing"

func TestReleaseProfileStable_GendocsCommandNotRegistered(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "gendocs" {
			t.Fatal("gendocs should not be registered in stable profile")
		}
	}
}
