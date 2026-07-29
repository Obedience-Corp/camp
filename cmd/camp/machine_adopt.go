package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var machineAdoptForce bool

// adoptIsTerminal and adoptConfirm are production seams for the consent path.
// Tests swap them so cancel vs explicit-No can be asserted without a real TTY.
var adoptIsTerminal = ui.IsTerminal
var adoptConfirm = confirmForm

// adoptDetectTailnet / adoptDetectBare are the dual reachable-name probes used
// by isSelfOrigin. Production uses tailscale Self + os.Hostname; tests inject
// pure functions so the dual-name matrix is covered without a live tailnet.
var adoptDetectTailnet = func(ctx context.Context) (string, error) {
	return detectReachableName(ctx, runTailscaleStatusForSelf)
}
var adoptDetectBare = func(ctx context.Context) (string, error) {
	return detectReachableName(ctx, nil)
}

var machineAdoptCmd = &cobra.Command{
	Use:   "adopt",
	Short: "Register the machine this session was hopped from",
	Long: `Register the machine this session was hopped from, after showing you exactly
what will be written.

A hop carries its origin in CAMP_HOP_ORIGIN. This reads that value and offers to
add the origin to your machines file so hops and completion work in the other
direction too. The hop itself never registers anything: adopting is an explicit,
interactive act, and there is no flag that skips the confirmation.

Adopting records how to REACH a machine. It does not make that machine reachable:
that depends on its own sshd or tailnet policy. Verify with 'camp machine diagnose'.

Answering No is remembered, so hints stay quiet until you ask again. Esc/cancel
aborts without writing decline memory. Re-running this command always works;
--force only skips the reminder that you declined before.`,
	Args: cobra.NoArgs,
	RunE: runMachineAdopt,
}

func runMachineAdopt(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		return camperrors.New("camp machine adopt does not support --json: registering a machine is " +
			"an explicit consent step, not a query")
	}

	origin, err := adoptOrigin()
	if err != nil {
		return err
	}
	// Fail closed on detection errors: a skipped self-guard could write a
	// machines.yaml row pointing at this host and break self-reference resolve.
	self, err := isSelfOrigin(ctx, origin)
	if err != nil {
		return camperrors.Wrap(err, "camp machine adopt: cannot determine whether this origin is this machine")
	}
	if self {
		return camperrors.New("camp machine adopt: this origin is this machine (" + origin.Host +
			"); nothing to adopt")
	}

	mf, err := machines.Load()
	if err != nil {
		return err
	}
	if existing, found := machineByHost(mf, origin.Host); found {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s is already in your fleet as %q; nothing to adopt\n",
			origin.Host, existing.ID)
		return err
	}

	entry, err := adoptEntry(mf, origin)
	if err != nil {
		return err
	}

	// The TTY refusal comes before any human-oriented output. A non-interactive
	// run that has already printed a decline reminder and then errors leaves
	// half a consent surface on stdout, which is exactly what the rest of this
	// command avoids.
	if !adoptIsTerminal() {
		return camperrors.New("camp machine adopt requires an interactive terminal: registering a " +
			"machine is an explicit consent step and cannot be automated")
	}

	declined, declineErr := machines.LoadDeclined()
	if declineErr != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: warning: %v (treating as no declines)\n", declineErr)
	}
	if d, was := declined.IsDeclined(origin.Host); was && !machineAdoptForce {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "You declined this origin on %s; adopting now will register it.\n\n",
			d.DeclinedAt.Format("2006-01-02"))
	}

	if _, err := fmt.Fprint(cmd.OutOrStdout(), adoptPreview(origin, entry)); err != nil {
		return err
	}
	ok, canceled, err := adoptConfirm(ctx, fmt.Sprintf("Add machine %q?", entry.ID))
	if err != nil {
		return err
	}
	if canceled {
		// Esc/abort is not a deliberate decline — leave decline memory alone.
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Not adopted.")
		return err
	}
	if !ok {
		declined.Decline(entry.ID, origin.Host, time.Now())
		if err := declined.Save(); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: warning: could not record the decline: %v\n", err)
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(),
			"Not adopted. Run 'camp machine adopt' again if you change your mind.")
		return err
	}

	mf.Upsert(entry)
	if err := mf.Save(); err != nil {
		return err
	}
	declined.Remove(origin.Host)
	if err := declined.Save(); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: warning: could not clear the decline record: %v\n", err)
	}

	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s machine %q adopted (%s, %s)\n  next: camp machine diagnose %s\n",
		ui.SuccessIcon(), entry.ID, entry.Host, entry.AuthMethod, entry.ID)
	return err
}

// adoptOrigin reads and parses the session payload, translating both failure
// shapes into messages that explain what the command is for. The likeliest
// reader of either error ran adopt on the wrong machine or outside a hop.
func adoptOrigin() (HopOrigin, error) {
	raw := strings.TrimSpace(os.Getenv(HopOriginEnvVar))
	if raw == "" {
		return HopOrigin{}, camperrors.New("camp machine adopt: no origin in this session (" +
			HopOriginEnvVar + " is not set); this command adopts the machine you hopped here from")
	}
	origin, err := ParseHopOrigin(raw)
	if err != nil {
		return HopOrigin{}, camperrors.New("camp machine adopt: " + HopOriginEnvVar +
			" is malformed (" + err.Error() + "); nothing to adopt")
	}
	return origin, nil
}

// adoptEntry maps the payload to a machines.yaml row. auth_method is never
// inferred from the payload (it carries none, deliberately): defaulting to
// ssh-agent is honest, and the preview says so. The label records provenance,
// which is the only way to tell a hop-suggested entry from a typed one later.
func adoptEntry(mf *machines.File, origin HopOrigin) (machines.Machine, error) {
	id := origin.ID
	if validateMachineID(id) != nil {
		id = suggestedMachineID(origin.Host)
	}
	if id == "" {
		return machines.Machine{}, camperrors.New("camp machine adopt: cannot derive a valid machine id from " +
			origin.Host + "; add it explicitly with 'camp machine add <id> --host " + origin.Host + "'")
	}
	if _, _, found := mf.Lookup(id); found {
		return machines.Machine{}, camperrors.New("camp machine adopt: machine id " + id +
			" is already used by a different host; add this one explicitly with " +
			"'camp machine add <id> --host " + origin.Host + "'")
	}
	auth, err := normalizeAuthMethod("")
	if err != nil {
		return machines.Machine{}, err
	}
	return machines.Machine{
		ID:         id,
		Label:      "adopted from hop " + time.Now().Format("2006-01-02"),
		Host:       origin.Host,
		AuthMethod: auth,
		SSHUser:    origin.User,
	}, nil
}

// adoptPreview renders the YAML that will be appended (via the same
// yaml.Marshal path File.Save uses), plus the reachability caveat. The
// operator approves the bytes, not a hand-formatted summary of them.
func adoptPreview(origin HopOrigin, entry machines.Machine) string {
	var b strings.Builder
	b.WriteString("\nOrigin from this session:\n\n")
	fmt.Fprintf(&b, "  host      %s\n", origin.Host)
	fmt.Fprintf(&b, "  user      %s\n", origin.User)
	if origin.Campaign != "" {
		fmt.Fprintf(&b, "  campaign  %s\n", origin.Campaign)
	}
	b.WriteString("\nThis entry will be appended to " + machines.MachinesPath() + ":\n\n")
	// Marshal the row the same way Save does so quoting/escaping match disk.
	yml, err := yaml.Marshal([]machines.Machine{entry})
	if err != nil {
		// Unreachable for a well-formed Machine; keep the preview usable.
		fmt.Fprintf(&b, "    - id: %s\n      host: %s\n", entry.ID, entry.Host)
	} else {
		for _, line := range strings.Split(strings.TrimRight(string(yml), "\n"), "\n") {
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	fmt.Fprintf(&b, "\n  auth_method is a default; change it with 'camp machine add %s --auth tailscale-ssh'\n", entry.ID)
	fmt.Fprintf(&b, "\nAdopting records how to reach %s. It does not make %s reachable:\n", entry.ID, entry.ID)
	fmt.Fprintf(&b, "that depends on its own sshd or tailnet policy. Verify with 'camp machine\ndiagnose %s' after adopting.\n\n", entry.ID)
	return b.String()
}

// machineByHost finds an entry for host regardless of id, so one machine cannot
// end up registered twice under two names. Two rows for one host would mean two
// ControlMaster sockets to the same place and an ambiguous machine list.
func machineByHost(mf *machines.File, host string) (machines.Machine, bool) {
	want := machines.NormalizeHost(host)
	for _, m := range mf.Machines {
		if machines.NormalizeHost(m.Host) == want {
			return m, true
		}
	}
	return machines.Machine{}, false
}

// isSelfOrigin guards the one entry that would break self-reference resolution:
// a machines.yaml row pointing at the adopting machine. The guard belongs here,
// at write time, because this is the only place such a row can be created.
//
// It compares against BOTH names this machine can legitimately be called: the
// tailnet name and the plain hostname. detectReachableName returns whichever is
// available, so a payload built when tailscale was down would carry the hostname
// while this machine now reports its tailnet name, and a single comparison would
// miss it. Both are this machine, so both must count.
func isSelfOrigin(ctx context.Context, origin HopOrigin) (bool, error) {
	want := machines.NormalizeHost(origin.Host)
	if want == "" {
		return false, nil
	}
	tailnet, err := adoptDetectTailnet(ctx)
	if err != nil {
		return false, err
	}
	if machines.NormalizeHost(tailnet) == want {
		return true, nil
	}
	bare, err := adoptDetectBare(ctx)
	if err != nil {
		return false, err
	}
	return machines.NormalizeHost(bare) == want, nil
}
