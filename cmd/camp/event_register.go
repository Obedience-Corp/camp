//go:build dev

package main

import (
	eventpkg "github.com/Obedience-Corp/camp/cmd/camp/event"
	"github.com/Obedience-Corp/camp/internal/commands/release"
)

func init() {
	release.MarkDevOnly(eventpkg.Cmd)
	rootCmd.AddCommand(eventpkg.Cmd)
}
