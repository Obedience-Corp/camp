package transfer

import "github.com/Obedience-Corp/camp/internal/machines"

// LocalPrefix forces the campaign reading of an endpoint whose leading segment
// would otherwise be read as a machine id. It is spelled "local" to match
// machines.LocalMachineID and `camp switch local:<campaign>`, so one reserved
// word means the same thing in both commands.
const LocalPrefix = machines.LocalMachineID

// Endpoint is a parsed transfer endpoint. Machine is empty for every form that
// exists today, which is what makes the new grammar additive: an unconfigured
// fleet can never produce a non-empty Machine.
type Endpoint struct {
	Machine  string // registered machine id, or "" for local
	Campaign string // campaign segment for a remote endpoint
	Path     string // path within the campaign for a remote endpoint
	Spec     string // the endpoint as the campaign resolver should see it
	Shadowed bool   // head resolved as BOTH a machine id and a campaign
}

// IsRemote reports whether this endpoint lives on another machine.
func (e Endpoint) IsRemote() bool { return e.Machine != "" }

// machineLookupFunc reports whether id is a registered machine. Injected so the
// grammar is testable without a machines.yaml on disk.
type machineLookupFunc func(id string) bool

// registeredMachine is the production lookup: machines.yaml, exactly the pair
// runRemoteSwitch uses.
func registeredMachine(id string) bool {
	if id == "" || id == machines.LocalMachineID {
		return false
	}
	mf, err := machines.Load()
	if err != nil {
		return false
	}
	_, _, found := mf.Lookup(id)
	return found
}

// campaignLookupFunc reports whether head resolves as a campaign, used only to
// detect shadowing so the note can name the escape hatch.
type campaignLookupFunc func(head string) bool

// ParseEndpoint applies the machine-first grammar. The behavior change is gated
// on the leading segment matching a REGISTERED machine id, so a user with no
// ~/.obey/machines.yaml gets byte-identical behavior: on an unconfigured fleet
// the new grammar is unreachable.
func ParseEndpoint(spec string, isMachine machineLookupFunc, isCampaign campaignLookupFunc) (Endpoint, error) {
	head, rest, hasColon := parseSpec(spec)
	if !hasColon {
		return Endpoint{Spec: spec}, nil
	}

	// "local:" forces the campaign reading. It is the escape hatch for a
	// campaign whose name or id prefix collides with a registered machine id;
	// without it, registering a machine could make a campaign unreachable.
	if head == LocalPrefix {
		return Endpoint{Spec: rest}, nil
	}

	if isMachine == nil || !isMachine(head) {
		return Endpoint{Spec: spec}, nil
	}

	campaign, path, hasSecond := parseSpec(rest)
	if !hasSecond || campaign == "" || path == "" {
		return Endpoint{}, errMachineNeedsCampaign(head, campaign)
	}
	shadowed := isCampaign != nil && isCampaign(head)
	return Endpoint{Machine: head, Campaign: campaign, Path: path, Spec: spec, Shadowed: shadowed}, nil
}

// ShadowNote is the stderr line emitted when a head resolves as both a machine
// and a campaign. It fires every time rather than once per process: it reports
// an ambiguity in the command the operator just typed, and a silently different
// reading of the second invocation is exactly what it exists to prevent.
func ShadowNote(machine string) string {
	return "camp: " + machine + " is a registered machine; reading it as machine:campaign:path " +
		"(use " + LocalPrefix + ":" + machine + ":... for the campaign)"
}

func errMachineNeedsCampaign(machine, campaign string) error {
	example := "notes.md"
	if campaign != "" {
		example = campaign
	}
	return newEndpointError(machine, example)
}

// BothRemoteError rejects a machine-to-machine copy. It is decidable from the
// two parsed endpoints alone, so failing here costs the user nothing, and camp
// cannot promise the two far machines can reach each other (D003).
func BothRemoteError(src, dest Endpoint) error {
	return newBothRemoteError(src.Machine, dest.Machine)
}

// ParseEndpointDefault is ParseEndpoint wired to the real machines.yaml and to a
// campaign check that only runs when a machine already matched, so the common
// case (no fleet configured) costs one cheap file read that Load already caches
// behind an absent-file fast path.
func ParseEndpointDefault(spec string) (Endpoint, error) {
	return ParseEndpoint(spec, registeredMachine, nil)
}
