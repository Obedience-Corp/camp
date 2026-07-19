//go:build dev

package main

import (
	auditpkg "github.com/Obedience-Corp/camp/cmd/camp/audit"
	"github.com/Obedience-Corp/camp/internal/commands/release"
)

func init() {
	release.MarkDevOnly(auditpkg.Cmd)
	rootCmd.AddCommand(auditpkg.Cmd)
}
