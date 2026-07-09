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
implicitly available as "local" and is never written to that file.`,
	Example: `  camp machine list
  camp machine add devbox --host devbox.tailnet.ts.net --auth tailscale-ssh
  camp machine add --discover
  camp machine remove devbox`,
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
tailnet device instead of specifying --host/--auth by hand; the chosen device
is saved with auth_method=tailscale-ssh. Pass an id positionally with
--discover to select that device by its derived id non-interactively (skips
the picker), or use --yes to take the first discovered device.`,
	Example: `  camp machine add devbox --host devbox.tailnet.ts.net --auth tailscale-ssh
  camp machine add buildbox --host 10.0.0.12 --auth ssh-agent --user ci
  camp machine add --discover
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

var (
	machineListJSON    bool
	machineAddHost     string
	machineAddLabel    string
	machineAddAuth     string
	machineAddUser     string
	machineAddIdentity string
	machineAddDiscover bool
	machineAddYes      bool
)

func init() {
	rootCmd.AddCommand(machineCmd)
	machineCmd.GroupID = "global"
	machineCmd.AddCommand(machineListCmd)
	machineCmd.AddCommand(machineAddCmd)
	machineCmd.AddCommand(machineRemoveCmd)

	machineListCmd.Flags().BoolVar(&machineListJSON, "json", false, "Output as JSON")

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
