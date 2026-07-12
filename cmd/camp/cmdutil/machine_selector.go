package cmdutil

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ParsedMachineSelector is a machine-qualified switch selector. Machine is "" for
// a local selector, in which case Sel is exactly what ParseSwitchSelector returns
// today (byte-identical). A non-empty Machine is looked up in ~/.obey/machines.yaml
// by the switch resolver. Remainder is the raw post-colon selector text, sent
// verbatim to the remote's own `camp switch --print` so nothing is lost
// re-rendering the parsed struct.
type ParsedMachineSelector struct {
	Machine   string
	Remainder string
	Sel       ParsedSwitchSelector
}

// ParseMachineSelector splits an optional "machine:" prefix on the first colon,
// then defers to the existing selector grammar for the remainder. Colon is parsed
// before @ and /, and ParseSwitchSelector never inspects ':' — so the machine
// dimension is collision-free with the shipped org/campaign[@tab] grammar, and a
// selector with no colon is byte-identical to today.
func ParseMachineSelector(raw string) (ParsedMachineSelector, error) {
	machine, remainder, hasMachine := strings.Cut(raw, ":")
	if !hasMachine {
		machine, remainder = "", raw
	} else {
		if machine == "" {
			return ParsedMachineSelector{}, camperrors.New("empty machine id before ':'")
		}
		if strings.ContainsAny(machine, "/@") {
			return ParsedMachineSelector{}, camperrors.New("machine id may not contain '/' or '@'")
		}
		if err := config.ValidateName("machine", machine); err != nil {
			return ParsedMachineSelector{}, camperrors.Wrap(err, "invalid machine id")
		}
	}
	return ParsedMachineSelector{
		Machine:   machine,
		Remainder: remainder,
		Sel:       ParseSwitchSelector(remainder),
	}, nil
}
