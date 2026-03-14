//go:build dev

package main

import (
	questpkg "github.com/Obedience-Corp/camp/cmd/camp/quest"
	"github.com/Obedience-Corp/camp/internal/commands/release"
)

func init() {
	release.MarkDevOnly(questpkg.Cmd)
	rootCmd.AddCommand(questpkg.Cmd)
}
