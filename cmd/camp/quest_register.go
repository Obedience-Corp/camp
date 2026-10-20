package main

import (
	questpkg "github.com/Obedience-Corp/camp/cmd/camp/quest"
)

func init() {
	rootCmd.AddCommand(questpkg.Cmd)
}
