package main

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// machineJSON mirrors machines.Machine's field names for JSON output
// (id/label/host/auth_method/ssh_user/identity_file) — the ~/.obey/machines.yaml
// schema names, not Go's default exported-field JSON encoding (which would emit
// "ID"/"AuthMethod"/... instead). omitempty on every non-id field lets the
// synthetic "local" row degrade to exactly {"id":"local"} instead of six
// mostly-empty keys.
type machineJSON struct {
	ID           string `json:"id"`
	Label        string `json:"label,omitempty"`
	Host         string `json:"host,omitempty"`
	AuthMethod   string `json:"auth_method,omitempty"`
	SSHUser      string `json:"ssh_user,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
}

func toMachineJSON(m machines.Machine) machineJSON {
	return machineJSON{
		ID:           m.ID,
		Label:        m.Label,
		Host:         m.Host,
		AuthMethod:   m.AuthMethod,
		SSHUser:      m.SSHUser,
		IdentityFile: m.IdentityFile,
	}
}

// machineListOutput is `camp machine list --json`'s payload shape: the file's
// version plus every machine, with LocalMachineID included as a synthetic
// {"id":"local"} row so a consumer sees the whole reachable fleet in one place
// rather than needing to know "local" is implicit. This is a deliberate,
// documented choice (see 01_machine_subcommands.md Step 2) distinct from
// machines.File itself, which never persists "local".
type machineListOutput struct {
	Version  int           `json:"version"`
	Machines []machineJSON `json:"machines"`
}

var machineCmd = &cobra.Command{
	Use:   "machine",
	Short: "Manage remote machines (~/.obey/machines.yaml)",
	Long: `Manage the fleet of remote machines camp can reach for 'camp switch machine:campaign'
and 'camp list --remote'.

Machines are stored in ~/.obey/machines.yaml. The current machine is always
implicitly available as "local" and is never written to that file.

Network vs login: Tailscale (or LAN) is how you reach the host; SSH auth is how
you log in. Prefer OpenSSH keys/agent (auth_method=ssh-agent) by default;
Tailscale SSH (auth_method=tailscale-ssh) is opt-in identity login. Terminal
hops always use BatchMode (agents never hang on password prompts).

'camp machine diagnose' reports auth mode, a copy-paste ssh probe, and
ControlMaster socket state (and can clear a stale socket with --reset).

Run without a subcommand in a terminal to manage the fleet interactively: add,
discover, edit, and remove machines, and see each one's socket state. The
subcommands stay the interface for scripts and agents, and remain what a
non-terminal 'camp machine' prints help for.`,
	Args: cobra.NoArgs,
	RunE: runMachineTUI,
	Example: `  camp machine
  camp machine list
  camp machine add buildbox --host 10.0.0.12 --auth ssh-agent --user ci
  camp machine add devbox --host devbox.tailnet.ts.net --auth tailscale-ssh
  camp machine add --discover
  camp machine add --discover --auth tailscale-ssh --user lance
  camp machine remove devbox
  camp machine diagnose
  camp machine diagnose devbox --reset`,
}

var machineListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List configured machines",
	Long: `List every machine in ~/.obey/machines.yaml, plus the implicit "local" machine
(this machine, never persisted to the file).`,
	RunE: runMachineList,
}

var machineAddCmd = &cobra.Command{
	Use:   "add [id]",
	Short: "Add or update a machine",
	Long: `Add a machine to ~/.obey/machines.yaml, or update it if the id already exists
(idempotent on id: a second 'add' with the same id replaces the entry rather
than duplicating it).

With --discover, camp runs 'tailscale status --json' and lets you pick a
tailnet device (network identity only). Default auth is OpenSSH keys/agent
(ssh-agent); pass --auth tailscale-ssh for Tailscale identity login. --user and
--identity are honored with --discover. Pass an id positionally with --discover
to select that device by its derived id non-interactively (skips the picker),
or use --yes to take the first discovered device.`,
	Example: `  camp machine add buildbox --host 10.0.0.12 --auth ssh-agent --user ci
  camp machine add devbox --host devbox.tailnet.ts.net --auth tailscale-ssh
  camp machine add --discover
  camp machine add --discover --auth tailscale-ssh --user lance
  camp machine add devbox --discover
  camp machine add --discover --yes`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMachineAdd,
}

var machineRemoveCmd = &cobra.Command{
	Use:     "remove <id>",
	Aliases: []string{"rm"},
	Short:   "Remove a machine",
	Long:    `Remove a machine from ~/.obey/machines.yaml. Removing "local" or an unknown id is an error.`,
	Args:    cobra.ExactArgs(1),
	RunE:    runMachineRemove,
}

var machineDiagnoseCmd = &cobra.Command{
	Use:   "diagnose [id]",
	Short: "Inspect machine auth, probe line, and ssh ControlMaster sockets",
	Long: `Report how each configured machine is set up to hop (or one machine if an id
is given):

  auth     OpenSSH (keys/agent) or Tailscale SSH (identity)
  probe    copy-paste BatchMode ssh line to test outside camp
  socket   ControlMaster multiplex state:
             none   no socket — the next hop opens a fresh master
             live   socket present and the master answers 'ssh -O check'
             stale  socket present but the master no longer answers

A stale socket is what a sleep or network flap can leave behind; until it is
removed (or ControlPersist expires) the next 'camp switch machine:...' or
'camp list --remote' hop to that machine can hang. Pass --reset to tear down
stale sockets so the next hop reconnects cleanly. Live and absent sockets are
left untouched.`,
	Example: `  camp machine diagnose
  camp machine diagnose devbox
  camp machine diagnose --reset
  camp machine diagnose devbox --reset --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMachineDiagnose,
}

var (
	machineListJSON      bool
	machineAddHost       string
	machineAddLabel      string
	machineAddAuth       string
	machineAddUser       string
	machineAddIdentity   string
	machineAddDiscover   bool
	machineAddYes        bool
	machineDiagnoseReset bool
	machineDiagnoseJSON  bool
)

func init() {
	rootCmd.AddCommand(machineCmd)
	machineCmd.GroupID = "global"
	machineCmd.AddCommand(machineListCmd)
	machineCmd.AddCommand(machineAddCmd)
	machineCmd.AddCommand(machineRemoveCmd)
	machineCmd.AddCommand(machineDiagnoseCmd)

	machineListCmd.Flags().BoolVar(&machineListJSON, "json", false, "Output as JSON")

	machineDiagnoseCmd.Flags().BoolVar(&machineDiagnoseReset, "reset", false, "Tear down stale ControlMaster sockets so the next hop reconnects")
	machineDiagnoseCmd.Flags().BoolVar(&machineDiagnoseJSON, "json", false, "Output as JSON")

	machineAddCmd.Flags().StringVar(&machineAddHost, "host", "", "SSH host or Tailscale MagicDNS name (required unless --discover)")
	machineAddCmd.Flags().StringVar(&machineAddLabel, "label", "", "Human-readable label")
	machineAddCmd.Flags().StringVar(&machineAddAuth, "auth", machines.AuthSSHAgent,
		fmt.Sprintf("Auth method: %s, %s, %s", machines.AuthTailscaleSSH, machines.AuthSSHAgent, machines.AuthSSHPassword))
	machineAddCmd.Flags().StringVar(&machineAddUser, "user", "", "SSH user")
	machineAddCmd.Flags().StringVar(&machineAddIdentity, "identity", "", "Path to SSH identity file")
	machineAddCmd.Flags().BoolVar(&machineAddDiscover, "discover", false, "Discover devices via 'tailscale status --json' and pick one")
	machineAddCmd.Flags().BoolVar(&machineAddYes, "yes", false, "With --discover, take the first discovered device non-interactively")
}

func runMachineList(cmd *cobra.Command, _ []string) error {
	mf, err := machines.Load()
	if err != nil {
		return err
	}
	if machineListJSON {
		return writeMachineListJSON(cmd.OutOrStdout(), mf)
	}
	return renderMachineListTable(cmd.OutOrStdout(), mf.Machines)
}

func writeMachineListJSON(w io.Writer, mf *machines.File) error {
	out := machineListOutput{
		Version:  mf.Version,
		Machines: make([]machineJSON, 0, len(mf.Machines)+1),
	}
	out.Machines = append(out.Machines, machineJSON{ID: machines.LocalMachineID})
	for _, m := range mf.Machines {
		out.Machines = append(out.Machines, toMachineJSON(m))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func renderMachineListTable(w io.Writer, ms []machines.Machine) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
		ui.Label("ID"), ui.Label("HOST"), ui.Label("AUTH"), ui.Label("")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
		ui.Label(machines.LocalMachineID), "", "", ui.Dim("(this machine)")); err != nil {
		return err
	}
	for _, m := range ms {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", ui.Label(m.ID), m.Host, m.AuthMethod, ""); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, ui.Dim(ui.CountLabel(len(ms)+1, "machine", "machines")))
	return err
}

func runMachineAdd(cmd *cobra.Command, args []string) error {
	if machineAddDiscover {
		return runMachineAddDiscover(cmd, args)
	}
	if machineAddYes {
		return camperrors.New("--yes only applies together with --discover")
	}
	if len(args) != 1 {
		return camperrors.New("machine add requires exactly one id argument (or --discover)")
	}
	id := args[0]
	if err := validateMachineID(id); err != nil {
		return err
	}
	if machineAddHost == "" {
		return camperrors.New("--host is required")
	}
	auth, err := normalizeAuthMethod(machineAddAuth)
	if err != nil {
		return err
	}

	mf, err := machines.Load()
	if err != nil {
		return err
	}
	mf.Upsert(machines.Machine{
		ID:           id,
		Label:        machineAddLabel,
		Host:         machineAddHost,
		AuthMethod:   auth,
		SSHUser:      machineAddUser,
		IdentityFile: machineAddIdentity,
	})
	if err := mf.Save(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s machine %q saved (%s, %s)\n", ui.SuccessIcon(), id, machineAddHost, auth)
	return err
}

func runMachineRemove(cmd *cobra.Command, args []string) error {
	id := args[0]
	if id == machines.LocalMachineID {
		return camperrors.New(`cannot remove "local"; it is the current machine, not a configured entry`)
	}

	mf, err := machines.Load()
	if err != nil {
		return err
	}
	idx := -1
	for i := range mf.Machines {
		if mf.Machines[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return camperrors.New(fmt.Sprintf("unknown machine %q", id))
	}
	mf.Machines = append(mf.Machines[:idx], mf.Machines[idx+1:]...)
	if err := mf.Save(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s machine %q removed\n", ui.SuccessIcon(), id)
	return err
}

// machineDiagnoseRow is one machine's status in `camp machine diagnose --json`.
// Reset is true when --reset cleared a stale socket for this machine on this run.
type machineDiagnoseRow struct {
	ID           string `json:"id"`
	Host         string `json:"host,omitempty"`
	AuthMethod   string `json:"auth_method,omitempty"`
	AuthLabel    string `json:"auth_label,omitempty"`
	SSHUser      string `json:"ssh_user,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
	Probe        string `json:"probe,omitempty"`
	Socket       string `json:"socket"`
	State        string `json:"state"`
	Reset        bool   `json:"reset"`
}

func runMachineDiagnose(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	mf, err := machines.Load()
	if err != nil {
		return err
	}

	targets := mf.Machines
	if len(args) == 1 {
		id := args[0]
		if id == machines.LocalMachineID {
			return camperrors.New(`"local" is this machine and has no ControlMaster socket`)
		}
		found := false
		for _, m := range mf.Machines {
			if m.ID == id {
				targets = []machines.Machine{m}
				found = true
				break
			}
		}
		if !found {
			return camperrors.New(fmt.Sprintf("unknown machine %q", id))
		}
	}

	rows := make([]machineDiagnoseRow, 0, len(targets))
	for i := range targets {
		m := &targets[i]
		d := remote.CheckControlMaster(ctx, m)
		row := machineDiagnoseRow{
			ID:           m.ID,
			Host:         m.Host,
			AuthMethod:   m.AuthMethod,
			AuthLabel:    remote.AuthDisplayName(m.AuthMethod),
			SSHUser:      m.SSHUser,
			IdentityFile: m.IdentityFile,
			Probe:        remote.ProbeCommand(m),
			Socket:       d.Socket,
			State:        string(d.State),
		}
		if machineDiagnoseReset && d.State == remote.ControlStale {
			if err := remote.ResetControlMaster(ctx, m); err != nil {
				return err
			}
			row.State = string(remote.ControlNone)
			row.Reset = true
		}
		rows = append(rows, row)
	}

	if machineDiagnoseJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Machines []machineDiagnoseRow `json:"machines"`
		}{Machines: rows})
	}
	return renderMachineDiagnoseTable(cmd.OutOrStdout(), rows)
}

func renderMachineDiagnoseTable(w io.Writer, rows []machineDiagnoseRow) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, ui.Dim("No machines configured; nothing to diagnose."))
		return err
	}
	reset := 0
	for i, r := range rows {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		state := r.State
		if r.Reset {
			state = r.State + " (was stale, cleared)"
			reset++
		}
		// Human output abbreviates $HOME so pastes do not leak full paths.
		// --json keeps absolute paths for consumers.
		lines := []string{
			fmt.Sprintf("%s  %s", ui.Label("ID"), r.ID),
			fmt.Sprintf("%s  %s", ui.Label("HOST"), r.Host),
			fmt.Sprintf("%s  %s (%s)", ui.Label("AUTH"), r.AuthLabel, r.AuthMethod),
		}
		if r.SSHUser != "" {
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("USER"), r.SSHUser))
		}
		if r.IdentityFile != "" {
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("IDENTITY"), pathutil.AbbreviateHome(r.IdentityFile)))
		}
		lines = append(lines,
			fmt.Sprintf("%s  %s", ui.Label("SOCKET"), state+" · "+pathutil.AbbreviateHome(r.Socket)),
			fmt.Sprintf("%s  %s", ui.Label("PROBE"), r.Probe),
		)
		for _, line := range lines {
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	if !machineDiagnoseReset && machineDiagnoseHasStale(rows) {
		if _, err := fmt.Fprintln(w, ui.Dim("Stale socket(s) found. Run 'camp machine diagnose --reset' to clear them.")); err != nil {
			return err
		}
	}
	if machineDiagnoseReset {
		if _, err := fmt.Fprintln(w, ui.Dim(ui.CountLabel(reset, "stale socket cleared", "stale sockets cleared"))); err != nil {
			return err
		}
	}
	return nil
}

func machineDiagnoseHasStale(rows []machineDiagnoseRow) bool {
	for _, r := range rows {
		if r.State == string(remote.ControlStale) {
			return true
		}
	}
	return false
}

// validateMachineID rejects the reserved "local" id and enforces the same
// lowercase-letters/digits/hyphens shape as other camp identifiers
// (internal/config/names.go), shared by manual 'add' and 'add --discover'.
func validateMachineID(id string) error {
	if id == machines.LocalMachineID {
		return camperrors.New(`machine id "local" is reserved for the current machine`)
	}
	if err := config.ValidateName("machine", id); err != nil {
		return camperrors.Wrap(err, "invalid machine id")
	}
	return nil
}

// normalizeAuthMethod defaults an empty --auth to ssh-agent and rejects any
// value outside the three machines package constants, listing the valid ones
// in the error so a typo is immediately actionable.
func normalizeAuthMethod(auth string) (string, error) {
	if auth == "" {
		return machines.AuthSSHAgent, nil
	}
	switch auth {
	case machines.AuthTailscaleSSH, machines.AuthSSHAgent, machines.AuthSSHPassword:
		return auth, nil
	default:
		return "", camperrors.New(fmt.Sprintf(
			"invalid --auth %q; must be one of: %s, %s, %s",
			auth, machines.AuthTailscaleSSH, machines.AuthSSHAgent, machines.AuthSSHPassword))
	}
}
