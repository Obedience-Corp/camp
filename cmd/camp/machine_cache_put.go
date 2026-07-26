package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// snapshotMaxNames caps how many campaign names one push may carry. The payload
// crosses as an argv, so the bound protects the receiver's command line; the
// rule is truncate-and-still-write rather than refuse, because a partial
// completion list is useful and an error is not.
const snapshotMaxNames = 500

// snapshotMaxBytes bounds the joined names for the same reason, far below any
// platform ARG_MAX.
const snapshotMaxBytes = 64 * 1024

var cachePutCampaigns []string

// machineCachePutCmd is machine-to-machine plumbing, not an operator verb, so it
// is hidden. A human who runs it by hand gets correct behavior; they are simply
// not advertised it.
var machineCachePutCmd = &cobra.Command{
	Use:    "cache-put <machine-id>",
	Short:  "Record another machine's campaign names in this machine's completion cache (internal)",
	Hidden: true,
	Long: `Record another machine's campaign names in this machine's completion cache.

Hosts call this over ssh after hopping here, so this machine can complete
'<id>:<campaign>' for them without ever connecting back. Names only: no paths, no
ids, no auth material. The receiver validates everything it is handed.`,
	Args: cobra.ExactArgs(1),
	RunE: runMachineCachePut,
}

func runMachineCachePut(cmd *cobra.Command, args []string) error {
	id := args[0]
	// Validated FIRST, and not merely as a naming nicety: the id becomes the
	// cache file name, so this is what keeps a hostile value inside the cache
	// directory.
	if err := validateMachineID(id); err != nil {
		return err
	}
	names, err := sanitizeSnapshotNames(cachePutCampaigns)
	if err != nil {
		return err
	}
	writeMachineSnapshotCampaigns(id, names)
	// Stdout stays empty on every path so a caller can distinguish "wrote" from
	// "said something" without parsing.
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "cached %d campaign names for %s\n", len(names), id)
	return nil
}

// sanitizeSnapshotNames validates and bounds the pushed names. Names are display
// and completion data on the receiver, never resolved as paths, and the
// separator and control-character rules keep them that way.
func sanitizeSnapshotNames(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	total := 0
	for _, chunk := range raw {
		for _, name := range strings.Split(chunk, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if len(name) > 255 || strings.ContainsAny(name, "/:\x00\n\r") {
				return nil, camperrors.New("invalid campaign name in snapshot: " + name)
			}
			if len(out) >= snapshotMaxNames || total+len(name) > snapshotMaxBytes {
				return out, nil
			}
			out = append(out, name)
			total += len(name)
		}
	}
	if len(out) == 0 {
		return nil, camperrors.New("no campaign names given (use --campaigns a,b,c)")
	}
	return out, nil
}

// snapshotPushTimeout bounds a push. It rides an already-established
// ControlMaster connection and sends a few KiB, so this is generous; and it sits
// in front of a hop the operator is waiting on, so the 10s control-plane default
// would be a visible regression.
const snapshotPushTimeout = 2 * time.Second

// pushSnapshotRun is the seam that lets the push's silence be tested without ssh.
var pushSnapshotRun = remote.RunCampCommand

// pushSelfSnapshot tells m about this machine's campaigns, so completion works
// in the other direction without m ever connecting back (D006).
//
// Every failure is silent by design. The operator asked to hop or to list, not
// to sync caches, so an unreachable machine, an older camp without the verb, a
// timeout, or a rejected payload must not surface. In particular an old remote
// exits 127 (POSIX command-not-found, the same signal campNotFoundHint keys on)
// or with cobra's unknown-command error, and both are swallowed here.
//
// It is called AFTER the hop line is written. The line is what the operator is
// waiting for; the push is bookkeeping, and bookkeeping does not go first.
func pushSelfSnapshot(ctx context.Context, m *machines.Machine, selfID string, names []string) {
	if m == nil || selfID == "" || len(names) == 0 {
		return
	}
	if validateMachineID(selfID) != nil {
		return
	}
	safe, err := sanitizeSnapshotNames(names)
	if err != nil || len(safe) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotPushTimeout)
	defer cancel()
	_, _ = pushSnapshotRun(ctx, m, "machine cache-put "+remote.ShellQuote(selfID)+
		" --campaigns "+remote.ShellQuote(strings.Join(safe, ",")))
}

// selfSnapshot returns this machine's id and campaign names for a push, or
// nothing when either is unavailable. Names only: no paths, no ids, no orgs, no
// auth material, and no list of other machines. Each exclusion is a thing that
// would otherwise leak to every machine this one hops to.
func selfSnapshot(ctx context.Context) (string, []string) {
	host, err := detectReachableName(ctx, runTailscaleStatusForSelf)
	if err != nil {
		return "", nil
	}
	id := suggestedMachineID(host)
	if id == "" {
		return "", nil
	}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return "", nil
	}
	all := reg.ListAll()
	names := make([]string, 0, len(all))
	for _, c := range all {
		if c.Name != "" {
			names = append(names, c.Name)
		}
	}
	sort.Strings(names)
	return id, names
}
