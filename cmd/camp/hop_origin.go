package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// HopOriginEnvVar carries the origin machine identity into a hopped session.
// A child process inherits it, so `camp switch -` and `camp machine adopt` on
// the far machine can name where the session came from without any reverse
// connection or sshd configuration. Mirrors the CAMP_QUEST pattern
// (internal/quest/shellenv.go) with more fields.
const HopOriginEnvVar = "CAMP_HOP_ORIGIN"

// hopOriginVersion prefixes every payload. A consumer that does not recognize
// the version ignores the whole payload rather than salvaging fields from it,
// so a future format cannot be half-read by an older camp.
const hopOriginVersion = "v1"

// Payload bounds. A field over its cap is dropped when optional and aborts the
// payload when required: a truncated hostname is a wrong hostname, and wrong
// routing is worse than absent routing.
const (
	hopOriginMaxTotal    = 1024
	hopOriginMaxHost     = 253
	hopOriginMaxUser     = 64
	hopOriginMaxCampaign = 255
	hopOriginMaxID       = 64
)

// hopOriginDetectTimeout bounds the tailscale probe. The hop already pays a
// full ssh round-trip for ResolveRoot (bounded by remote.DefaultTimeout, 10s),
// so this is a stall guard rather than a budget.
const hopOriginDetectTimeout = 2 * time.Second

// HopOrigin is a parsed CAMP_HOP_ORIGIN payload.
type HopOrigin struct {
	Host     string
	User     string
	Campaign string
	ID       string
}

// buildHopOrigin assembles the v1 payload describing this machine, for
// embedding in a hop to another machine. originCampaign may be empty (the hop
// started outside a campaign); host and user may not, and an empty result means
// "emit no origin", never a partial payload.
func buildHopOrigin(ctx context.Context, originCampaign string, detect reachableNameFunc) (string, error) {
	host, err := detectReachableName(ctx, detect)
	if err != nil {
		return "", err
	}
	if host == "" {
		return "", camperrors.New("no reachable name for this machine")
	}
	u, err := user.Current()
	if err != nil {
		return "", camperrors.Wrap(err, "resolve current user")
	}
	return encodeHopOrigin(HopOrigin{
		Host:     host,
		User:     u.Username,
		Campaign: originCampaign,
		ID:       suggestedMachineID(host),
	})
}

// encodeHopOrigin renders a payload, enforcing the caps. Pure, so the size and
// encoding rules are testable without touching the network.
func encodeHopOrigin(o HopOrigin) (string, error) {
	host := hopOriginEncode(o.Host)
	usr := hopOriginEncode(o.User)
	if host == "" || len(host) > hopOriginMaxHost {
		return "", camperrors.New("origin host is empty or too long")
	}
	if usr == "" || len(usr) > hopOriginMaxUser {
		return "", camperrors.New("origin user is empty or too long")
	}

	parts := []string{hopOriginVersion, "host=" + host, "user=" + usr}
	if c := hopOriginEncode(o.Campaign); c != "" && len(c) <= hopOriginMaxCampaign {
		parts = append(parts, "campaign="+c)
	}
	if id := hopOriginEncode(o.ID); id != "" && len(id) <= hopOriginMaxID {
		parts = append(parts, "id="+id)
	}

	payload := strings.Join(parts, ";")
	if len(payload) > hopOriginMaxTotal {
		// Drop optional fields rather than truncate: a truncated value is a
		// wrong value, and every consumer treats absence as "unknown".
		payload = strings.Join(parts[:3], ";")
		if len(payload) > hopOriginMaxTotal {
			return "", camperrors.New("origin payload exceeds size bound")
		}
	}
	if strings.ContainsAny(payload, "\n\r") {
		return "", camperrors.New("origin payload contains a newline")
	}
	return payload, nil
}

// ParseHopOrigin reads a payload. A malformed payload is an error rather than a
// best-effort parse: the value routes a hop, and a half-understood route is the
// failure mode the size rules already refuse.
func ParseHopOrigin(payload string) (HopOrigin, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return HopOrigin{}, camperrors.New("empty payload")
	}
	if len(payload) > hopOriginMaxTotal {
		return HopOrigin{}, camperrors.New("payload exceeds 1024 bytes")
	}
	tokens := strings.Split(payload, ";")
	if tokens[0] != hopOriginVersion {
		return HopOrigin{}, camperrors.New("unknown payload version")
	}

	var o HopOrigin
	seen := make(map[string]bool, len(tokens))
	for _, tok := range tokens[1:] {
		key, raw, ok := strings.Cut(tok, "=")
		if !ok || key == "" {
			return HopOrigin{}, camperrors.New("token without '='")
		}
		if seen[key] {
			return HopOrigin{}, camperrors.New("duplicate key \"" + key + "\"")
		}
		seen[key] = true
		val, err := hopOriginDecode(raw)
		if err != nil {
			return HopOrigin{}, err
		}
		// Reject C0 controls after decode so encode/parse stay symmetric:
		// encode percent-encodes them, but a handcrafted env could still
		// inject %0A/%0D and break the one-line shell-connect contract.
		if hopOriginHasC0(val) {
			return HopOrigin{}, camperrors.New("field \"" + key + "\" contains a control character")
		}
		switch key {
		case "host":
			o.Host = val
		case "user":
			o.User = val
		case "campaign":
			o.Campaign = val
		case "id":
			o.ID = val
		}
		// Unknown keys are ignored so additive v1 fields do not break older
		// consumers; a change in meaning bumps the version instead.
	}
	if o.Host == "" {
		return HopOrigin{}, camperrors.New("missing host")
	}
	if o.User == "" {
		return HopOrigin{}, camperrors.New("missing user")
	}
	return o, nil
}

// hopOriginHasC0 reports whether s contains a C0 control byte (including
// newline/CR). Mirrors the encode-side newline guard so a hostile env cannot
// smuggle controls past ParseHopOrigin via percent-encoding.
func hopOriginHasC0(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 {
			return true
		}
	}
	return false
}

// hopOriginEncode percent-encodes everything outside the unreserved set, which
// means ';', '=', '%', whitespace, control bytes, and non-ASCII can never
// appear raw in a value. The separators are therefore unambiguous by
// construction rather than by convention, and a hostile campaign name cannot
// inject a second field.
func hopOriginEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if hopOriginUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexUpper[c>>4])
		b.WriteByte(hexUpper[c&0x0f])
	}
	return b.String()
}

func hopOriginDecode(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", camperrors.New("invalid percent-encoding")
		}
		hi, ok1 := unhex(s[i+1])
		lo, ok2 := unhex(s[i+2])
		if !ok1 || !ok2 {
			return "", camperrors.New("invalid percent-encoding")
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), nil
}

const hexUpper = "0123456789ABCDEF"

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

func hopOriginUnreserved(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '.', c == '_', c == '-', c == '~':
		return true
	}
	return false
}

// reachableNameFunc runs `tailscale status --json` and returns raw stdout.
// Injected so detection is testable without a live tailscale binary, mirroring
// tailscaleStatusFunc (machine_discover.go).
type reachableNameFunc func(ctx context.Context) ([]byte, error)

// detectReachableName answers two questions with one derivation: what this
// machine calls itself when embedding an origin, and whether a selector's
// machine segment refers to this machine. One function so the two answers
// cannot drift.
//
// Tailscale's own DNSName wins when the backend is running, because it is
// reachable from anywhere on the tailnet; os.Hostname is the fallback and may
// well be non-routable (a mac reports "Mac-Studio.local"), which is why every
// consumer treats the value as advisory and shows it before acting on it.
func detectReachableName(ctx context.Context, run reachableNameFunc) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if run != nil {
		probeCtx, cancel := context.WithTimeout(ctx, hopOriginDetectTimeout)
		defer cancel()
		if out, err := run(probeCtx); err == nil {
			if name := selfDNSName(out); name != "" {
				return name, nil
			}
		}
	}
	host, err := os.Hostname()
	if err != nil {
		return "", camperrors.Wrap(err, "resolve hostname")
	}
	return normalizeDNSName(host), nil
}

// selfDNSName reads the Self node out of `tailscale status --json`.
// parseTailscaleStatus deliberately DROPS Self (its caller is a picker of other
// devices), so this is a sibling rather than a change to that function.
func selfDNSName(data []byte) string {
	start := bytes.IndexByte(data, '{')
	if start < 0 {
		return ""
	}
	var status tailscaleStatus
	if json.Unmarshal(data[start:], &status) != nil {
		return ""
	}
	if status.BackendState != "" && status.BackendState != "Running" {
		return ""
	}
	if status.Self == nil {
		return ""
	}
	return normalizeDNSName(status.Self.DNSName)
}

// runTailscaleStatusForSelf is the production reachableNameFunc. A missing
// binary or a non-zero exit yields no name and the caller falls back to
// hostname; unlike `machine add --discover`, self-detection is best-effort and
// must never turn a working hop into an error.
func runTailscaleStatusForSelf(ctx context.Context) ([]byte, error) {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "tailscale", "status", "--json")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// suggestedMachineID derives an advisory machine id from a reachable name.
// Empty when the name cannot produce a valid id (an IP literal, say): the field
// is optional, and adopt prompts rather than registering a bad id.
func suggestedMachineID(host string) string {
	id := sanitizeID(host)
	if validateMachineID(id) != nil {
		return ""
	}
	return id
}

// hopBackSelector is the reserved argument for the hop-back gesture. It is
// checked before selector parsing because `-` is otherwise a legal campaign
// query that reaches the fuzzy tier and matches an arbitrary hyphenated
// campaign name, differently on every invocation.
const hopBackSelector = "-"

// transientOriginSuffix namespaces the in-memory machine built from a payload.
// The id becomes a ControlMaster socket name, so colliding with a REGISTERED
// machine id would make ssh reuse that machine's live master and land the hop
// on the wrong host. A registered id ending in "-origin" would have to be
// created deliberately.
const transientOriginSuffix = "-origin"

// runHopBack implements `camp switch -`: return to the machine and campaign this
// session was hopped from. Registration-independent by design — the origin need
// never have been adopted here — so it reads the payload and builds a transient
// machine rather than consulting ~/.obey/machines.yaml. Nothing is written.
func runHopBack(ctx context.Context, cmd *cobra.Command, printOnly, shellConnect, jsonOut bool) error {
	raw := os.Getenv(HopOriginEnvVar)
	if strings.TrimSpace(raw) == "" {
		return camperrors.New("camp switch -: this session did not start from a camp hop (" +
			HopOriginEnvVar + " is not set)")
	}
	origin, err := ParseHopOrigin(raw)
	if err != nil {
		return camperrors.New("camp switch -: " + HopOriginEnvVar + " is malformed (" + err.Error() +
			"); hop back with 'camp switch <machine>:<campaign>'")
	}
	if origin.Campaign == "" {
		return camperrors.New("camp switch -: origin campaign unknown (the outbound hop did not start " +
			"inside a campaign); hop back with 'camp switch <machine>:<campaign>'")
	}

	m, registered := originTarget(origin)
	if err := guardRemoteOutputFlags(m.ID, printOnly, jsonOut); err != nil {
		return err
	}
	root, err := resolveRemoteRoot(ctx, m, origin.Campaign)
	if err != nil {
		return hopBackFailure(err, m, registered)
	}
	return emitHopOrRefuse(ctx, cmd, m, origin.Campaign, root, shellConnect)
}

// originTarget resolves the payload to a machine to hop to. A registered entry
// matching the origin host wins: it carries the operator's own identity_file,
// ssh_user, and auth_method, all better information than the payload has. This
// is the expected steady state once a fleet is mutually adopted.
//
// Otherwise the machine is transient and in-memory. AuthMethod is left empty on
// purpose: the hop argv is identical for tailscale-ssh and ssh-agent (authArgs
// branches only on IdentityFile), EnsureKeyAuth rejects only ssh-password, and
// failure classification reads ssh's own stderr. Guessing an auth mode would put
// a wrong label on the one message an operator reads when a hop breaks.
func originTarget(origin HopOrigin) (*machines.Machine, bool) {
	want := strings.ToLower(normalizeDNSName(origin.Host))
	if mf, err := machines.Load(); err == nil {
		for i := range mf.Machines {
			if strings.ToLower(normalizeDNSName(mf.Machines[i].Host)) == want {
				return &mf.Machines[i], true
			}
		}
	}
	return &machines.Machine{
		ID:      transientOriginID(origin.Host),
		Label:   origin.Host,
		Host:    origin.Host,
		SSHUser: origin.User,
	}, false
}

// transientOriginID derives the in-memory machine's id. It is only two things:
// a ControlMaster path component and a name in error text. Both tolerate a
// generic value; neither tolerates an empty one, which would collapse the socket
// path to ~/.obey/ssh-ctl/.sock and share one socket across every degenerate
// origin — the exact cross-host reuse the suffix exists to prevent.
func transientOriginID(host string) string {
	if id := suggestedMachineID(host); id != "" {
		return id + transientOriginSuffix
	}
	return strings.TrimPrefix(transientOriginSuffix, "-")
}

// hopBackFailure explains an unreachable origin. The diagnostic pointer differs
// by registration because `camp machine diagnose` only knows registered
// machines: pointing an operator at it for a transient origin sends them to a
// command that answers "unknown machine".
func hopBackFailure(err error, m *machines.Machine, registered bool) error {
	if registered {
		return camperrors.Wrapf(err, "run 'camp machine diagnose %s' to check reachability", m.ID)
	}
	return camperrors.Wrapf(err, "the origin is not registered here, probe it with: %s", remote.ProbeCommand(m))
}

// isSelfMachine reports whether a selector's machine segment names THIS
// machine, by comparing against the id derived from this machine's reachable
// name — character for character the derivation that fills the payload's id
// field, so the two answers cannot disagree.
//
// Ordering matters and is deliberate. This is checked only AFTER
// machines.LocalMachineID and only for a selector that would otherwise hop, so
// the detection cost (a tailscale probe) never lands on a successful local
// switch. A registered id wins over self-detection: Lookup(id) only — any
// machines.yaml row whose id equals the selector suppresses local resolve,
// regardless of that row's host. That honors the operator's explicit id
// mapping; adopt refuses to create a true self-row, but a shared fleet file
// that lists every node under its real id (including this one) will hop via
// ssh rather than short-circuit locally.
func isSelfMachine(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}
	if mf, err := machines.Load(); err == nil {
		if _, _, found := mf.Lookup(id); found {
			return false
		}
	}
	host, err := detectReachableName(ctx, runTailscaleStatusForSelf)
	if err != nil || host == "" {
		return false
	}
	return suggestedMachineID(host) == id
}
