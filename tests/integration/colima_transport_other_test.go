//go:build integration && !darwin && !linux

package integration

func startDedicatedColimaDockerTransport() (func(), error) {
	return func() {}, nil
}
