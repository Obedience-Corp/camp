package transfer

import camperrors "github.com/Obedience-Corp/camp/internal/errors"

// newEndpointError explains a machine endpoint that is missing its campaign
// segment. Falling back to the campaign reading would look for a campaign named
// after the machine, fail with "camp not found", and send the operator
// hunting for a campaign when the real problem is a missing segment.
func newEndpointError(machine, example string) error {
	return camperrors.New("\"" + machine + "\" is a machine; use machine:campaign:path " +
		"(for example " + machine + ":<camp>:" + example + ")")
}

func newBothRemoteError(srcMachine, destMachine string) error {
	return camperrors.New("at most one endpoint may be on another machine (got " +
		srcMachine + " and " + destMachine + ")")
}
