package fresh

import (
	"io"
	"os"
	"sync"

	"github.com/Obedience-Corp/camp/internal/ui/interaction"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// wrapInteraction runs the actual Fresh command, streaming its ordinary output
// and CLI-owned questions as versioned NDJSON. Responses arrive on stdin.
// Capture is process-scoped and used only by the CLI entry point, never a server.
func wrapInteraction(cmd *cobra.Command) {
	run := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		enabled, _ := cmd.Flags().GetBool("interaction-json")
		if !enabled {
			return run(cmd, args)
		}
		if term.IsTerminal(int(os.Stdin.Fd())) {
			state, err := term.MakeRaw(int(os.Stdin.Fd()))
			if err != nil {
				return err
			}
			defer term.Restore(int(os.Stdin.Fd()), state) //nolint:errcheck
		}
		stdout, stderr := os.Stdout, os.Stderr
		reader, writer, err := os.Pipe()
		if err != nil {
			return err
		}
		ctx, session := interaction.New(cmd.Context(), os.Stdin, stdout)
		defer session.Close()
		if err := session.Emit(interaction.Event{Type: "ready"}); err != nil {
			_ = reader.Close()
			_ = writer.Close()
			return err
		}
		out, errOut, oldCtx := cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.Context()
		os.Stdout, os.Stderr = writer, writer
		cmd.SetOut(writer)
		cmd.SetErr(writer)
		cmd.SetContext(ctx)
		var drained sync.WaitGroup
		drained.Add(1)
		go func() { defer drained.Done(); _, _ = io.Copy(session, reader) }()
		err = run(cmd, args)
		os.Stdout, os.Stderr = stdout, stderr
		cmd.SetOut(out)
		cmd.SetErr(errOut)
		cmd.SetContext(oldCtx)
		_ = writer.Close()
		drained.Wait()
		_ = reader.Close()
		if err == nil {
			err = ctx.Err()
		}
		complete := interaction.Event{Type: "complete"}
		if err != nil {
			complete.Error = err.Error()
		}
		if emitErr := session.Emit(complete); err == nil {
			err = emitErr
		}
		// Root error printing goes to stderr; stdout remains a valid protocol.
		return err
	}
}
