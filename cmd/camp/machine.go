package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/tailnet"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/version"
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

The mesh model, in one paragraph: hopping exports CAMP_HOP_ORIGIN into the
remote login shell, a single v1 line naming where the shell came from, which is
what lets 'camp switch -' return without either machine storing state about the
other. A payload does not register anything: if the origin is unknown here,
camp names 'camp machine adopt', which previews and asks, requires a TTY, and
remembers a decline. Reachability need not be symmetric -- a machine you cannot
dial still appears in completion, because visibility travels as a snapshot
pushed during a successful enumerate rather than as a live query. Camp will
never install a key, register a machine unattended, or claim a host is reachable
on the strength of tailnet membership alone.

See docs/machine-mesh.md for the reachability matrix (notably: the Tailscale SSH
server does not run in sandboxed macOS GUI builds, so a mac accepts OpenSSH keys
instead) and docs/transfer.md for the machine-first transfer grammar.

Run without a subcommand in a terminal to manage the fleet interactively: add,
discover, edit, and remove machines, see each one's socket state, and press
enter to pick a camp on the selected machine and hop to it. Hopping needs
the shell wrapper ('eval "$(camp shell-init zsh)"'), because no subprocess can
replace the shell it was run from; without it the screen says so rather than
appearing to work. The subcommands stay the interface for scripts and agents,
and remain what a non-terminal 'camp machine' prints help for.`,
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
  resolve  whether the host becomes an address at all, checked before any ssh
           is attempted. A MagicDNS name that will not resolve reports
           tailscale's own health text as the reason. When the local tailnet
           peer table still knows the machine's address, camp dials that
           address instead (pinning the host key to the configured name) and
           the line shows which address a hop will actually use; otherwise
           the remote camp version probe is skipped instead of blaming a
           machine that was never addressable. Set CAMP_NO_PEER_FALLBACK=1
           to disable the fallback and fail exactly as ssh would
  socket   ControlMaster multiplex state:
             none   no socket: the next hop opens a fresh master
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
	machineCmd.AddCommand(machineAdoptCmd)
	machineCmd.AddCommand(machineCachePutCmd)
	// StringArray (not StringSlice): each --campaigns is one name intact, so
	// names may contain commas. The push path emits repeated flags to match.
	machineCachePutCmd.Flags().StringArrayVar(&cachePutCampaigns, "campaigns", nil,
		"Camp name on the calling machine (repeatable; names may contain commas)")
	machineAdoptCmd.Flags().BoolVar(&machineAdoptForce, "force", false,
		"Skip the reminder that you declined this origin before (never skips the confirmation)")

	// Shared with `camp list`: the TUI writes the chosen destination here and the
	// shell wrapper acts on it, because a subprocess cannot move its parent shell.
	machineCmd.Flags().String("path-output", "", "Write the selected hop target to a file (shell integration)")
	_ = machineCmd.Flags().MarkHidden("path-output")

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
		// JSON consumers get data, never advice: the hint is suppressed here
		// rather than inside OriginHint, because whether advice belongs in the
		// output is the caller's decision.
		return writeMachineListJSON(cmd.OutOrStdout(), mf)
	}
	if err := renderMachineListTable(cmd.OutOrStdout(), mf.Machines); err != nil {
		return err
	}
	// OriginHint's contract is non-TTY silence: piped/redirected list stays pure
	// data, and the once-per-process token is not burned on non-interactive paths.
	// --json already returns above; this covers bare table output without a TTY.
	if ui.IsTerminal() {
		if hint := OriginHint(); hint != "" {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), ui.Dim(hint))
			return err
		}
	}
	return nil
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
	Hint         string `json:"hint,omitempty"`
	Socket       string `json:"socket"`
	State        string `json:"state"`
	Reset        bool   `json:"reset"`
	CampVersion  string `json:"camp_version,omitempty"`
	CampCommit   string `json:"camp_commit,omitempty"`
	// CampPath is where the far machine's camp binary was found, and
	// CampOnPath whether the account's login shell finds it unaided (false
	// means the hop works only because camp fell back to its usual install
	// locations). CampMissing is the login-shell PATH and every usual
	// location coming up empty: ssh worked, camp is not there. Additive with
	// omitempty on the strings, so existing --json consumers are unaffected.
	CampPath     string `json:"camp_path,omitempty"`
	CampOnPath   bool   `json:"camp_on_path"`
	CampOverride bool   `json:"camp_path_override,omitempty"`
	CampMissing  bool   `json:"camp_missing"`
	// CheckURL is the Tailscale approval URL when that is why the probe could
	// not reach the machine. Additive and omitempty, so existing --json
	// consumers are unaffected.
	CheckURL    string `json:"check_url,omitempty"`
	VersionSkew bool   `json:"version_skew"`
	// ReverseCapable reports whether THIS machine accepts inbound ssh, which is
	// the only half of reverse reachability camp can observe without executing
	// on the far side. Additive with omitempty on the hint so existing --json
	// consumers are unaffected.
	ReverseCapable bool   `json:"reverse_capable"`
	ReverseHint    string `json:"reverse_hint,omitempty"`
	// Resolves reports whether Host became an address before any ssh was
	// attempted; ResolveHint names the cause when it did not. Additive with
	// omitempty on the detail fields so existing --json consumers are
	// unaffected. ResolveChecked separates "no host to look up" from "looked,
	// and it failed", which the bare bool cannot express.
	ResolveChecked bool     `json:"resolve_checked"`
	Resolves       bool     `json:"resolves"`
	ResolveAddrs   []string `json:"resolve_addrs,omitempty"`
	ResolveHint    string   `json:"resolve_hint,omitempty"`
	// DialHost is the address camp actually hands ssh; DialViaPeer marks it as
	// a tailnet peer-table fallback for a host that did not resolve. Additive
	// with omitempty, so existing --json consumers are unaffected.
	DialHost    string `json:"dial_host,omitempty"`
	DialViaPeer bool   `json:"dial_via_peer"`
}

// probeRemoteCampVersion asks the machine's own camp for its version. It is
// best-effort: any failure (unreachable, camp missing, ancient binary without
// `version --json`) reports as an empty version, never a diagnose error —
// diagnose must keep working against exactly the broken machines it exists for.
// The hop is reuse-only: under ControlMaster=auto this probe would unlink a
// stale socket and open a fresh master, so diagnose would heal the exact stale
// state it is reporting and the follow-up --reset would find nothing to clear.
// The probe also reports a Tailscale approval URL when that is what stopped it.
// Without this, diagnose printed a hint telling the operator to "look for a
// login.tailscale.com check URL" while holding the one it had just been handed.
func probeRemoteCampVersion(ctx context.Context, m *machines.Machine) (versionStr, commit, checkURL string) {
	out, err := remote.RunCampCommandReuseOnly(ctx, m, "version --json")
	if err != nil {
		return "", "", remote.TailscaleCheckURL(err)
	}
	var info version.Info
	if err := json.Unmarshal(out, &info); err != nil {
		return "", "", ""
	}
	return info.Version, info.Commit, ""
}

// probeRemoteCamp asks the machine where its camp is before asking that camp
// for its version, so diagnose can tell "ssh failed" from "ssh worked, no
// camp there" from "camp ran" — three states the old single probe collapsed
// into one "unavailable". Ordering also keeps a dead host to one timeout:
// when the location probe fails at the ssh level, the version probe would
// only fail the same way, so it is skipped and the location error supplies
// any Tailscale check URL. missing is true when the far side exhausted the
// login-shell PATH and camp's usual install locations (exit 127).
func probeRemoteCamp(ctx context.Context, m *machines.Machine) (loc remote.CampLocation, versionStr, commit, checkURL string, missing bool) {
	loc, err := remote.RemoteCampLocation(ctx, m)
	switch {
	case err == nil:
		versionStr, commit, checkURL = probeRemoteCampVersion(ctx, m)
		return loc, versionStr, commit, checkURL, false
	case remote.IsCampNotFound(err):
		// With an override in force the miss is that path, not the fallback
		// list, and the BINARY line should say which path was tried.
		if p := os.Getenv(remote.RemoteCampPathEnv); p != "" {
			return remote.CampLocation{Path: p, Override: true}, "", "", "", true
		}
		return remote.CampLocation{}, "", "", "", true
	default:
		return remote.CampLocation{}, "", "", remote.TailscaleCheckURL(err), false
	}
}

// campVersionSkew reports whether a probed remote version differs from this
// binary. Builds carry a git-describe version, so two commits normally differ
// by version alone. The commit is still compared because builds can share a
// version string: two builds of one tag, or an explicit VERSION override.
func campVersionSkew(local version.Info, remoteVersion, remoteCommit string) bool {
	if remoteVersion == "" {
		return false
	}
	if remoteVersion != local.Version {
		return true
	}
	return remoteCommit != "" && local.Commit != "" && remoteCommit != local.Commit
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

	localInfo := version.Get()
	// Reverse reachability is a fact about this machine, identical for every
	// row. Probing it inside the loop cost N loopback dials and N tailscale
	// probes against a 2s budget each to learn the same answer.
	reverse := checkReverseReachability(ctx, defaultReverseProbes())
	// Built once, not per row: the probes memoize the tailscale health read, so
	// a fleet whose MagicDNS is down pays for that answer a single time.
	resolveProbeSet := defaultResolveProbes()
	rows := make([]machineDiagnoseRow, 0, len(targets))
	for i := range targets {
		m := &targets[i]
		d := remote.CheckControlMaster(ctx, m)
		resolved := checkHostResolves(ctx, m.Host, resolveProbeSet)
		// The dial decision, made through the same code path a hop uses — but
		// fed the lookup result already in hand, so diagnose does not pay for
		// (or race against) a second live resolution.
		endpoint := remote.ResolveEndpointWith(ctx, m, remote.EndpointProbes{
			LookupHost: func(context.Context, string) ([]string, error) {
				if resolved.Resolved {
					return resolved.Addrs, nil
				}
				return nil, errHostDidNotResolve
			},
			PeerAddress: tailnet.PeerAddress,
		})
		// A host that does not resolve — and has no peer-table fallback —
		// cannot be dialed, so the version probe would spend its ssh budget
		// proving what the lookup already showed, and then report the failure
		// as "camp missing / too old", which is the wrong diagnosis. Skipping
		// it makes the broken case faster and honest. An unprobed version is
		// recorded exactly as a failed probe is: writeMachineVersionCache
		// ignores an empty version either way.
		var remoteVersion, remoteCommit, checkURL string
		var campLoc remote.CampLocation
		var campMissing bool
		if !resolved.Checked || resolved.Resolved || endpoint.ViaPeer {
			campLoc, remoteVersion, remoteCommit, checkURL, campMissing = probeRemoteCamp(ctx, m)
		}
		// Diagnose is the only surface that pays for a live probe, so it is the
		// only one that can warm the cache the hop path reads.
		writeMachineVersionCache(m.ID, remoteVersion, remoteCommit)
		row := machineDiagnoseRow{
			ID:           m.ID,
			Host:         m.Host,
			AuthMethod:   m.AuthMethod,
			AuthLabel:    remote.AuthDisplayName(m.AuthMethod),
			SSHUser:      m.SSHUser,
			IdentityFile: m.IdentityFile,
			Probe:        endpoint.ProbeCommand(),
			Hint:         remote.AuthModeHint(m),
			Socket:       d.Socket,
			State:        string(d.State),
			CampVersion:  remoteVersion,
			CampCommit:   remoteCommit,
			CampPath:     campLoc.Path,
			CampOnPath:   campLoc.OnPATH,
			CampOverride: campLoc.Override,
			CampMissing:  campMissing,
			CheckURL:     checkURL,
			VersionSkew:  campVersionSkew(localInfo, remoteVersion, remoteCommit),
			// What this machine can honestly say about being reachable FROM
			// that one. Identical for every row (it is a fact about here, not
			// about them), and reported per row because that is where an
			// operator is already looking when a hop-back fails.
			ReverseCapable: reverse.Capable,
			ReverseHint:    reverse.Hint,
			ResolveChecked: resolved.Checked,
			Resolves:       resolved.Resolved,
			ResolveAddrs:   resolved.Addrs,
			ResolveHint:    resolved.Hint,
			DialHost:       endpoint.DialHost,
			DialViaPeer:    endpoint.ViaPeer,
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

// machineDiagnoseBinaryLine renders where the far machine's camp is, or that
// there is none, so the operator can tell a working-by-fallback hop from a
// clean one and a missing binary from a dead host. Empty when the location
// was never probed (host unresolvable, ssh failed, or check-mode blocked it).
func machineDiagnoseBinaryLine(r machineDiagnoseRow) string {
	switch {
	case r.CampMissing && r.CampOverride:
		return fmt.Sprintf("%s  ✗ %s %s", ui.Label("BINARY"), r.CampPath,
			"("+remote.RemoteCampPathEnv+") is not an executable on that machine")
	case r.CampMissing:
		return fmt.Sprintf("%s  ✗ %s", ui.Label("BINARY"),
			"not found on the login-shell PATH or in "+remote.CampInstallDirsDisplay()+
				"; install camp there, or set "+remote.RemoteCampPathEnv+" to its exact path")
	case r.CampPath == "":
		return ""
	case r.CampOverride:
		return fmt.Sprintf("%s  %s %s", ui.Label("BINARY"), r.CampPath, ui.Dim("("+remote.RemoteCampPathEnv+")"))
	case r.CampOnPath:
		return fmt.Sprintf("%s  %s %s", ui.Label("BINARY"), r.CampPath, ui.Dim("(on the login-shell PATH)"))
	default:
		return fmt.Sprintf("%s  %s %s", ui.Label("BINARY"), r.CampPath,
			ui.Dim("(found in a usual install location, not on the login-shell PATH; hops work, but interactive shells may run a different camp)"))
	}
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
		// Directly above CAMP, because when this fails it is the reason CAMP
		// has nothing to report, and the two read as cause and effect in order.
		if r.ResolveChecked {
			mark, detail := "✓", strings.Join(r.ResolveAddrs, ", ")
			switch {
			case r.Resolves:
			case r.DialViaPeer:
				// The name is still broken — say so — but camp has an address,
				// so the line leads with what will actually be dialed.
				detail = r.DialHost + " (via tailnet peer table; " + r.ResolveHint + ")"
			default:
				mark, detail = "✗", r.ResolveHint
			}
			lines = append(lines, fmt.Sprintf("%s  %s %s", ui.Label("RESOLVE"), mark, detail))
		}
		if line := machineDiagnoseBinaryLine(r); line != "" {
			lines = append(lines, line)
		}
		switch {
		case r.ResolveChecked && !r.Resolves && !r.DialViaPeer:
			// Never "camp missing / too old" for a host that was never
			// addressable: that sends the operator to the far machine to fix
			// something, when nothing on it is wrong.
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("CAMP"),
				ui.Dim("not probed (the host does not resolve)")))
		case r.CheckURL != "":
			// Naming the cause beats the generic "unavailable": nothing is
			// broken here, the hop is waiting on a browser approval.
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("CAMP"),
				ui.Dim("blocked on a Tailscale SSH check")))
		case r.CampMissing:
			// ssh got in and found nothing to run. Not "unreachable": the
			// network and auth are fine, the far machine needs camp.
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("CAMP"),
				ui.Dim("not probed (no camp binary to run; see BINARY)")))
		case r.CampVersion == "" && r.CampPath == "":
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("CAMP"),
				ui.Dim("unavailable (the machine could not be reached)")))
		case r.CampVersion == "":
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("CAMP"),
				ui.Dim("found, but 'version --json' failed (too old to report a version?)")))
		case r.VersionSkew:
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("CAMP"),
				campVersionDisplay(r)+"  ⚠ differs from this machine ("+campLocalVersionDisplay()+"); remote errors may not match current behavior"))
		default:
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("CAMP"), campVersionDisplay(r)))
		}
		if r.ReverseHint != "" {
			mark := "✗"
			if r.ReverseCapable {
				mark = "✓"
			}
			lines = append(lines, fmt.Sprintf("%s  %s %s", ui.Label("REVERSE"), mark, r.ReverseHint))
		}
		// Before the generic auth hint: this is the exact link that hint tells
		// the operator to go looking for, and it expires, so it leads.
		if r.CheckURL != "" {
			lines = append(lines,
				fmt.Sprintf("%s  %s", ui.Label("APPROVE"), r.CheckURL),
				fmt.Sprintf("%s  %s", ui.Label(""), ui.Dim("open it, approve, then run this again")))
		}
		// The Tailscale hint tells the operator to go looking for a check URL.
		// Once APPROVE has printed one, repeating that is worse than silence.
		if r.Hint != "" && r.CheckURL == "" {
			lines = append(lines, fmt.Sprintf("%s  %s", ui.Label("HINT"), r.Hint))
		}
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

// campVersionDisplay formats a probed remote camp version, appending the
// commit whenever the probe returned one. Versions are git-describe strings and
// usually differ on their own; the commit still separates builds that share a
// version string, such as two builds of one tag or a matching VERSION override.
func campVersionDisplay(r machineDiagnoseRow) string {
	if r.CampCommit != "" {
		return r.CampVersion + " (" + r.CampCommit + ")"
	}
	return r.CampVersion
}

func campLocalVersionDisplay() string {
	info := version.Get()
	if info.Commit != "" {
		return info.Version + " (" + info.Commit + ")"
	}
	return info.Version
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
