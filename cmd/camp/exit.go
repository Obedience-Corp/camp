package main

import (
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// silentExit reports the exit code to propagate without printing, and whether
// to do so.
//
// A command that returns a CommandError DIRECTLY is propagating a child
// process's exit code (camp sync, doctor, clone, run) and has already said
// whatever needed saying; printing again would be redundant.
//
// The type assertion is load-bearing, and errors.As would be wrong here.
// Remote failures create a CommandError deep in the chain (remote.Run wraps a
// non-zero ssh exit), and camp then wraps THAT with the sentence the operator
// actually needs: which machine, which campaign, which diagnostic to run next.
// Matching anywhere in the chain discarded all of it and exited silently with
// ssh's code, so a failed hop printed nothing at all.
//
// This lives beside main rather than in the test that exercises it, so the rule
// cannot pass in one place while production does something else.
func silentExit(err error) (int, bool) {
	cmdErr, ok := err.(*camperrors.CommandError)
	if !ok || cmdErr.ExitCode == 0 {
		return 0, false
	}
	return cmdErr.ExitCode, true
}
