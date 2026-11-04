package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/spf13/cobra"
)

const hopFollowEnv = "CAMP_APP_FOLLOW_HOP"
const hopMachineEnv = "CAMP_HOP_MACHINE_ID"
const maxHopSequence = uint64(9007199254740991)

type hopObservation struct {
	HopID    string `json:"hopId"`
	Sequence uint64 `json:"sequence"`
	CampID   string `json:"campId"`
	Root     string `json:"root"`
	CWD      string `json:"cwd"`
}

func (r *hopObservation) validate() error {
	if r == nil || len(r.HopID) != 16 || r.Sequence == 0 || r.Sequence >= maxHopSequence || r.CampID == "" || strings.Contains(r.CampID, ":") || !strings.HasPrefix(r.Root, "/") || !strings.HasPrefix(r.CWD, "/") {
		return camperrors.New("hop-resume: invalid camp observation")
	}
	for _, s := range []string{r.HopID, r.CampID, r.Root, r.CWD} {
		if len(s) > 4096 || strings.IndexFunc(s, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return camperrors.New("hop-resume: invalid observation text")
		}
	}
	return nil
}

func init() {
	report := &cobra.Command{Use: "report", Hidden: true, Args: cobra.NoArgs}
	report.Flags().Uint64("sequence", 0, "Shell observation sequence")
	report.RunE = func(cmd *cobra.Command, _ []string) error {
		seq, _ := cmd.Flags().GetUint64("sequence")
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		root, err := campaign.DetectFromCwd(cmd.Context())
		if err != nil {
			return err
		}
		cfg, err := config.ReadCampaignConfig(cmd.Context(), root)
		if err != nil {
			return err
		}
		observation := &hopObservation{os.Getenv(hopFollowEnv), seq, cfg.ID, root, cwd}
		if err := observation.validate(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()
		return sendHopObservation(ctx, observation)
	}
	cwd := &cobra.Command{Use: "cwd", Hidden: true, Args: cobra.NoArgs}
	cwd.Flags().Uint64("sequence", 0, "Shell directory sequence")
	cwd.RunE = func(cmd *cobra.Command, _ []string) error {
		id := os.Getenv(hopFollowEnv)
		seq, _ := cmd.Flags().GetUint64("sequence")
		if len(id) != 16 || seq == 0 || seq >= maxHopSequence {
			return camperrors.New("hop-resume: invalid cwd scope")
		}
		path, err := os.Getwd()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(struct {
			Version  int    `json:"version"`
			HopID    string `json:"hopId"`
			Sequence uint64 `json:"sequence"`
			CWD      string `json:"cwd"`
		}{1, id, seq, path})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "\x1b]777;camp-cwd;%s\a", raw)
		return err
	}
	hopResumeCmd.AddCommand(report, cwd)
}

func sendHopObservation(ctx context.Context, report *hopObservation) error {
	conn, relay, err := dialHopResume(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	request := hopResumeRequest{Observation: report}
	if relay {
		return sendOverHopRelay(conn, request)
	}
	if err := json.NewEncoder(conn).Encode(hopResumeEnvelope{Version: 1, Request: request}); err != nil {
		return err
	}
	var response hopResumeResponse
	if err := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&response); err != nil {
		return err
	}
	if response.Version != 1 || !response.Accepted {
		return camperrors.New("hop-resume: app following failed: " + printableHopText(response.Error))
	}
	return nil
}

// The local coordinator, never the remote report, supplies the machine and
// native terminal binding. Only this hop's private relay can reach prepare.
func followHop(ctx context.Context, hop *hopLine, id string, out io.Writer, prepare func(context.Context, hopResumeRequest) (string, error)) (func(context.Context, hopResumeRequest) (string, error), func()) {
	noop := func() {}
	if os.Getenv("FESTIVAL_APP_TERMINAL_BINDING") == "" {
		return prepare, noop
	}
	machine := hop.machineID()
	if machine == "" {
		return prepare, noop
	}
	if err := callHopApp(ctx, "start", id, 0, []string{"--machine-id", machine}, out); err != nil {
		_, _ = fmt.Fprintf(out, "camp: SSH will continue; app remote following unavailable: %v\r\n", err)
		return prepare, noop
	}
	hop.remoteCmd = "export " + hopFollowEnv + "=" + remote.ShellQuote(id) + " && " + hop.remoteCmd
	// Older or unconfigured remote shell integration keeps a working SSH
	// session, but must not look like a successful App follow.
	reported, stopped := make(chan struct{}), make(chan struct{})
	var reportOnce sync.Once
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-reported:
		case <-stopped:
		case <-timer.C:
			_, _ = fmt.Fprintln(out, "camp: remote shell has not reported its camp. Update Camp and enable camp shell-init in the remote shell to follow it in the app.")
		}
	}()
	var mu sync.Mutex
	var sequence uint64
	wrapped := func(ctx context.Context, r hopResumeRequest) (string, error) {
		if r.Observation == nil {
			return prepare(ctx, r)
		}
		mu.Lock()
		defer mu.Unlock()
		o := r.Observation
		if err := o.validate(); err != nil {
			return "", err
		}
		if o.HopID != id || o.Sequence <= sequence {
			return "", camperrors.New("hop-resume: stale observation")
		}
		// Reserve the sequence even on uncertain acknowledgement; a retry is a new
		// explicit shell report, never a replay of a potentially completed transfer.
		sequence = o.Sequence
		reportOnce.Do(func() { close(reported) })
		err := callHopApp(ctx, "entered", id, o.Sequence, []string{"--camp-id", o.CampID, "--root", o.Root, "--cwd", o.CWD}, out)
		return "", err
	}
	end := func() {
		close(stopped)
		// SSH failure/cancellation must still revoke the scope before local polling
		// resumes. The caller's cancelled context cannot be used for this cleanup.
		if err := callHopApp(context.WithoutCancel(ctx), "end", id, maxHopSequence, nil, out); err != nil {
			_, _ = fmt.Fprintf(out, "camp: app hop cleanup was not confirmed: %v\r\n", err)
		}
	}
	return wrapped, end
}
func callHopApp(ctx context.Context, phase, id string, seq uint64, extra []string, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"terminal", "hop", "--phase", phase, "--hop-id", id, "--sequence", strconv.FormatUint(seq, 10), "--timeout", "5s"}
	args = append(args, extra...)
	cmd := exec.CommandContext(ctx, "camp-app", args...)
	cmd.Stderr = out
	// stdout contains the protocol result, not terminal output or shell code.
	if err := cmd.Run(); err != nil {
		return camperrors.Wrap(err, "local app acknowledgement")
	}
	return nil
}
func (h hopLine) machineID() string {
	header, _, _ := strings.Cut(h.remoteCmd, " && ")
	tokens, err := splitShellQuoted(header)
	if err != nil || len(tokens) < 2 || tokens[0] != "export" || !strings.HasPrefix(tokens[1], hopMachineEnv+"=") {
		return ""
	}
	return strings.TrimPrefix(tokens[1], hopMachineEnv+"=")
}

// App-opened remote terminals use the same coordinator and onward route as
// shell-init, with stdout attached to their PTY rather than command substitution.
func init() {
	hopResumeCmd.AddCommand(&cobra.Command{Use: "terminal <ssh-line>", Hidden: true, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		line := args[0]
		for strings.HasPrefix(line, "ssh ") {
			next, err := runHopResume(cmd.Context(), line)
			if err != nil {
				return err
			}
			line = next
		}
		if strings.TrimSpace(line) == "" {
			return nil
		}
		tokens, err := splitShellQuoted(line)
		if err != nil || len(tokens) != 3 || tokens[0] != "cd" || tokens[1] != "--" {
			return camperrors.New("hop-resume: invalid local return route")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		follow := exec.CommandContext(ctx, "camp-app", "terminal", "switch-completed", "--timeout", "5s")
		follow.Dir = tokens[2]
		follow.Stderr = cmd.ErrOrStderr()
		if err := follow.Run(); err != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "camp: local shell resumed; app following was not confirmed")
		}
		cancel()
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		resumed := exec.CommandContext(cmd.Context(), shell, "-il")
		resumed.Dir = tokens[2]
		resumed.Stdin = cmd.InOrStdin()
		resumed.Stdout = cmd.OutOrStdout()
		resumed.Stderr = cmd.ErrOrStderr()
		return resumed.Run()
	}})
}
