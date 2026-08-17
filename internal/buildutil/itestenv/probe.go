package itestenv

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const (
	// probeSamples is enough to see a daemon that is intermittently stalling
	// without turning preflight into a benchmark.
	probeSamples = 5
	// probePingTimeout bounds one round trip when the caller supplies no
	// deadline of its own.
	probePingTimeout = 2 * time.Second
	// probeBudget bounds the whole probe. Refusing up front costs seconds;
	// collapsing mid-run costs the whole suite plus the triage that follows.
	probeBudget = 15 * time.Second

	// degradedMedian is the line between "busy" and "do not start a run here".
	// A healthy Colima daemon answers /_ping in 2 to 3 ms measured on the
	// development machine on 2026-08-16, so this sits roughly 200x above
	// healthy: it cannot fire on ordinary load, only on a daemon that is
	// already in the state that ends runs.
	degradedMedian = 500 * time.Millisecond

	// defaultDockerSocket is Docker's own default when DOCKER_HOST is unset.
	defaultDockerSocket = "/var/run/docker.sock"

	pingPath     = "/_ping"
	pingOKBody   = "OK"
	tcpScheme    = "tcp"
	httpScheme   = "http"
	dockerAPIURL = "http://docker" + pingPath
)

// errProbeUnsupported marks a transport this package cannot measure (a TLS or
// SSH Docker endpoint). Not being able to measure a daemon is not evidence
// that it is sick, so it never blocks a run.
var errProbeUnsupported = camperrors.New("Docker transport cannot be probed")

// ProbeResult is one preflight measurement of the daemon's responsiveness.
type ProbeResult struct {
	// DockerHost is the daemon that was measured.
	DockerHost string
	// Samples holds each successful round trip, in order.
	Samples []time.Duration
	// Median is the middle sample, zero when nothing succeeded.
	Median time.Duration
	// Err is the failure that ended the probe early, if any.
	Err error
}

// Probe measures a handful of Docker API round trips before a run commits to
// building a container pool.
//
// The 2026-08-05 incident is what this exists for: the suite dispatched a full
// run into a daemon that was already wedged, then spent fifteen minutes
// discovering that one exec at a time.
func Probe(ctx context.Context, dockerHost string) ProbeResult {
	result := ProbeResult{DockerHost: dockerHost}
	ctx, cancel := context.WithTimeout(ctx, probeBudget)
	defer cancel()

	for range probeSamples {
		start := time.Now()
		if err := Ping(ctx, dockerHost); err != nil {
			result.Err = err
			break
		}
		result.Samples = append(result.Samples, time.Since(start))
	}
	if len(result.Samples) > 0 {
		sorted := slices.Clone(result.Samples)
		slices.Sort(sorted)
		result.Median = sorted[len(sorted)/2]
	}
	return result
}

// Degraded reports whether a run should refuse to start, and why. It is pure
// so the thresholds stay tested without a daemon.
func (r ProbeResult) Degraded() (bool, string) {
	switch {
	case camperrors.Is(r.Err, errProbeUnsupported):
		return false, "daemon responsiveness not measured: " + r.Err.Error()
	case r.Err != nil && len(r.Samples) == 0:
		return true, "the Docker daemon at " + r.hostLabel() + " did not answer: " + r.Err.Error()
	case r.Err != nil:
		return true, "the Docker daemon at " + r.hostLabel() + " stopped answering after " +
			itoaSamples(len(r.Samples)) + ": " + r.Err.Error()
	case r.Median > degradedMedian:
		return true, "the Docker daemon at " + r.hostLabel() + " is answering in " +
			r.Median.Round(time.Millisecond).String() + " (healthy is single-digit ms); " +
			"it is out of headroom before this run even starts"
	default:
		return false, ""
	}
}

// Line renders the probe for a log or a doctor report.
func (r ProbeResult) Line() string {
	if len(r.Samples) == 0 {
		reason := "no response"
		if r.Err != nil {
			reason = r.Err.Error()
		}
		return "daemon probe: " + r.hostLabel() + " " + reason
	}
	return "daemon probe: " + r.hostLabel() + " median " +
		r.Median.Round(time.Microsecond).String() + " over " +
		itoaSamples(len(r.Samples))
}

func (r ProbeResult) hostLabel() string {
	if strings.TrimSpace(r.DockerHost) == "" {
		return "Docker's default socket"
	}
	return r.DockerHost
}

func itoaSamples(n int) string {
	if n == 1 {
		return "1 round trip"
	}
	return strconv.Itoa(n) + " round trips"
}

// Ping performs one Docker API round trip against dockerHost. It honours the
// caller's deadline when there is one, which is what lets a startup poll loop
// reuse it with a short budget.
func Ping(ctx context.Context, dockerHost string) error {
	client, endpoint, err := pingClient(dockerHost)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, probePingTimeout)
		defer cancel()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return camperrors.Wrap(err, "build Docker ping request")
	}
	response, err := client.Do(request)
	if err != nil {
		return camperrors.Wrap(err, "ping the Docker daemon")
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return camperrors.Wrap(err, "read the Docker ping response")
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != pingOKBody {
		return camperrors.Newf("Docker ping returned %s: %s",
			response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// pingClient builds a client for whichever transport dockerHost names. Unix
// sockets and plain TCP are the two the harness ever uses; anything else is
// reported as unmeasurable rather than guessed at.
func pingClient(dockerHost string) (*http.Client, string, error) {
	host := strings.TrimSpace(dockerHost)
	if host == "" {
		host = unixScheme + "://" + defaultDockerSocket
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, "", camperrors.Wrapf(err, "parse Docker host %q", dockerHost)
	}
	switch u.Scheme {
	case unixScheme:
		socket := u.Path
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, unixScheme, socket)
			},
		}
		return &http.Client{Transport: transport}, dockerAPIURL, nil
	case tcpScheme, httpScheme:
		return &http.Client{Transport: &http.Transport{}},
			httpScheme + "://" + u.Host + pingPath, nil
	default:
		return nil, "", camperrors.Wrapf(errProbeUnsupported, "scheme %q", u.Scheme)
	}
}
