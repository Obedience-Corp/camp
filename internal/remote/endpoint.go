package remote

import (
	"context"
	"net"
	"os"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/tailnet"
)

// Endpoint is one resolved dial decision for a machine: the address ssh should
// actually be handed, and whether that address came from the tailnet peer
// table instead of the configured host. It exists because Target and the opts
// builders are always used as a pair, and neither can know on its own that the
// other fell back — the pairing has to be carried by a value.
//
// Machine.Host is never mutated: configured state and observed state stay
// separate, and every log, error, and diagnose line keeps reporting the host
// the operator actually wrote.
type Endpoint struct {
	Machine *machines.Machine
	// DialHost is Machine.Host, or a peer-table address when ViaPeer.
	DialHost string
	// ViaPeer marks a fallback dial. It gates HostKeyAlias so the host key
	// stays filed under the stable MagicDNS name, not the address.
	ViaPeer bool
}

// NoPeerFallbackEnv, when set (any non-empty value), disables the peer-table
// dial fallback so camp fails exactly the way bare ssh would — the escape
// hatch for an operator debugging their own DNS. Same env-var pattern as
// CAMP_REMOTE_CAMP_PATH.
const NoPeerFallbackEnv = "CAMP_NO_PEER_FALLBACK"

// endpointResolveTimeout bounds the dial-path name lookup, mirroring the
// diagnose probe budget: a resolver that cannot answer in 2s has answered.
const endpointResolveTimeout = 2 * time.Second

// Direct returns the endpoint that dials the configured host, with no
// resolution attempted. It is the compatibility anchor: Target(m) and the
// package-level opts builders are equivalent to Direct(m)'s methods, so any
// call site not yet migrated keeps today's exact behavior.
func Direct(m *machines.Machine) Endpoint {
	return Endpoint{Machine: m, DialHost: m.Host}
}

// EndpointProbes are the seams ResolveEndpointWith is tested through, and the
// hook diagnose uses to reuse a lookup it has already performed instead of
// paying for a second one.
type EndpointProbes struct {
	LookupHost  func(ctx context.Context, host string) ([]string, error)
	PeerAddress func(ctx context.Context, dnsName string) (string, bool)
}

func defaultEndpointProbes() EndpointProbes {
	return EndpointProbes{
		LookupHost: func(ctx context.Context, host string) ([]string, error) {
			return net.DefaultResolver.LookupHost(ctx, host)
		},
		PeerAddress: tailnet.PeerAddress,
	}
}

// ResolveEndpoint decides how to dial m. DNS wins when it works; the peer
// table is consulted only for a MagicDNS name that failed to resolve; every
// path that cannot produce a better address returns Direct(m) so ssh still
// runs and still emits its own diagnostic. Camp never invents a reachability
// claim.
func ResolveEndpoint(ctx context.Context, m *machines.Machine) Endpoint {
	return ResolveEndpointWith(ctx, m, defaultEndpointProbes())
}

// ResolveEndpointWith is ResolveEndpoint with injected probes.
func ResolveEndpointWith(ctx context.Context, m *machines.Machine, p EndpointProbes) Endpoint {
	e := Direct(m)
	host := strings.TrimSpace(m.Host)
	// An address literal is already an address; an empty host has nothing to
	// resolve. Neither consults the resolver at all.
	if host == "" || net.ParseIP(host) != nil {
		return e
	}
	if os.Getenv(NoPeerFallbackEnv) != "" || p.LookupHost == nil || p.PeerAddress == nil {
		return e
	}
	lctx, cancel := context.WithTimeout(ctx, endpointResolveTimeout)
	addrs, err := p.LookupHost(lctx, host)
	cancel()
	// An empty answer with a nil error is a failure: a name that resolves to
	// nothing is not one ssh can dial.
	if err == nil && len(addrs) > 0 {
		return e
	}
	// Only a MagicDNS name has a second source of truth. Guessing an address
	// for an ordinary hostname would be worse than failing.
	if !tailnet.IsMagicDNSName(host) {
		return e
	}
	if addr, ok := p.PeerAddress(ctx, host); ok {
		e.DialHost = addr
		e.ViaPeer = true
	}
	return e
}

// Target returns the ssh destination: user@dialhost when ssh_user is set, else
// the dial host. Mirrors the app's ssh_target (remote/connection.rs:209-214)
// in construction; the dial host itself may be a camp-resolved fallback
// address, which is camp's own resilience layer on top of the shared
// construction.
func (e Endpoint) Target() string {
	if e.Machine.SSHUser != "" {
		return e.Machine.SSHUser + "@" + e.DialHost
	}
	return e.DialHost
}

// hostKeyAliasOpts pins ssh's known_hosts identity to the configured MagicDNS
// name when the dial went to a peer-table address. Without it, accept-new
// would not fail — it would silently file a second key under the IP, and host
// verification would come to rest on the less stable identity.
func (e Endpoint) hostKeyAliasOpts() []string {
	if !e.ViaPeer {
		return nil
	}
	return []string{"-o", "HostKeyAlias=" + tailnet.NormalizeDNSName(e.Machine.Host)}
}

// Opts is Opts(m) plus the fallback's HostKeyAlias when ViaPeer.
func (e Endpoint) Opts() []string {
	return append(Opts(e.Machine), e.hostKeyAliasOpts()...)
}

// OptsReuseOnly is OptsReuseOnly(m) plus the fallback's HostKeyAlias when
// ViaPeer.
func (e Endpoint) OptsReuseOnly() []string {
	return append(OptsReuseOnly(e.Machine), e.hostKeyAliasOpts()...)
}

// ProbeCommand returns the copy-paste BatchMode ssh line for this endpoint. It
// must reproduce what camp actually dials — alias and all — or the line
// diagnose tells operators to paste stops isolating the same failure.
func (e Endpoint) ProbeCommand() string {
	if e.Machine == nil {
		return ""
	}
	parts := []string{"ssh", "-o", "BatchMode=yes"}
	if e.Machine.IdentityFile != "" {
		parts = append(parts, "-o", "IdentitiesOnly=yes", "-i", expandTilde(e.Machine.IdentityFile))
	}
	parts = append(parts, e.hostKeyAliasOpts()...)
	parts = append(parts, e.Target(), "true")
	return strings.Join(parts, " ")
}

// TransferTarget renders the scp/rsync remote operand ([user@]host:path). A
// bare IPv6 dial host is bracketed — `fd7a::1:path` would parse the last hextet
// as the path.
func (e Endpoint) TransferTarget(remotePath string) string {
	host := e.DialHost
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		host = "[" + host + "]"
	}
	if e.Machine.SSHUser != "" {
		host = e.Machine.SSHUser + "@" + host
	}
	return host + ":" + remotePath
}

// FallbackNotice is the one dim line an interactive hop prints (to stderr —
// the hop's stdout is eval'd by the shell wrapper) when a fallback engaged, so
// a working hop still tells the operator their DNS is broken. Empty when there
// is nothing to say.
func (e Endpoint) FallbackNotice() string {
	if !e.ViaPeer {
		return ""
	}
	return e.Machine.Host + " did not resolve; dialing tailnet peer address " + e.DialHost +
		" (DNS on this machine is broken — run 'camp machine diagnose " + e.Machine.ID + "')"
}
