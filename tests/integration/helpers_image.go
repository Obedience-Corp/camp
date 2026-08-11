//go:build integration
// +build integration

package integration

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

// baseImageDockerfile bakes everything a pool member used to install and
// configure at startup. Before this image existed, every run sent four
// containers to the network for `apk add git jq` (the single flakiest setup
// step: it fails the whole run on a bad connection) and spent three more
// exec round trips per member on git config and directory creation.
//
// Pin to a specific digest rather than a floating tag. alpine:latest
// resolves to a different layer on every cache miss, which means the git
// version inside the container can silently change between runs. That
// matters for worktree and submodule tests where git semantics differ
// across minor versions. When bumping, update the digest here; the image
// tag is content-derived, so the new image builds automatically on the
// next run.
//
// jq is installed so --json output is validated by a real JSON consumer
// rather than only by our own unmarshaler, which would accept shapes a
// caller's parser rejects.
const baseImageDockerfile = `FROM alpine:3.21@sha256:f27cad9117495d32d067133afff942cb2dc745dfe9163e949f6bfe8a6a245339
RUN apk add --no-cache git jq \
 && git config --global user.email test@test.com \
 && git config --global user.name "Test User" \
 && mkdir -p /test /campaigns /root/.config/camp
`

// baseImageTag derives the image tag from the Dockerfile content, so editing
// the Dockerfile automatically builds a fresh image while an unchanged one
// reuses the daemon's cache with no version bookkeeping.
func baseImageTag() string {
	sum := sha256.Sum256([]byte(baseImageDockerfile))
	return "camp-itest-base:" + hex.EncodeToString(sum[:6])
}

// ensureBaseImage returns the pooled-container base image, building it on
// the daemon only when the content-derived tag is absent. After the first
// build, runs perform one image inspect and no network access.
func ensureBaseImage(ctx context.Context) (string, error) {
	tag := baseImageTag()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return "", fmt.Errorf("base image: docker provider: %w", err)
	}
	defer func() { _ = provider.Close() }()
	cli := provider.Client()

	if _, err := cli.ImageInspect(ctx, tag); err == nil {
		return tag, nil
	}

	// Build context is a one-file tar: the Dockerfile above.
	var buildContext bytes.Buffer
	tw := tar.NewWriter(&buildContext)
	if err := tw.WriteHeader(&tar.Header{
		Name: "Dockerfile",
		Mode: 0o644,
		Size: int64(len(baseImageDockerfile)),
	}); err != nil {
		return "", fmt.Errorf("base image: tar header: %w", err)
	}
	if _, err := tw.Write([]byte(baseImageDockerfile)); err != nil {
		return "", fmt.Errorf("base image: tar write: %w", err)
	}
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("base image: tar close: %w", err)
	}

	resp, err := cli.ImageBuild(ctx, &buildContext, client.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: "Dockerfile",
		Remove:     true,
	})
	if err != nil {
		return "", fmt.Errorf("base image: build request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The body is a JSON message stream; the daemon reports build failure as
	// an in-stream error object, not as a non-2xx response, so it must be
	// drained and checked or a failed build would go unnoticed until every
	// container create fails on a missing tag.
	dec := json.NewDecoder(resp.Body)
	for {
		var msg struct {
			Error string `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("base image: reading build output: %w", err)
		}
		if msg.Error != "" {
			return "", fmt.Errorf("base image: build failed: %s", msg.Error)
		}
	}

	if _, err := cli.ImageInspect(ctx, tag); err != nil {
		return "", fmt.Errorf("base image: built but not present as %s: %w", tag, err)
	}
	return tag, nil
}
