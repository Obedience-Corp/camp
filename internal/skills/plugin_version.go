package skills

import (
	"cmp"
	"strconv"
	"strings"
)

// pluginVersion is a plugin release X.Y.Z, or a development build Commits
// commits after it, as git describe prints it: X.Y.Z-N-g<hash>.
type pluginVersion struct {
	major, minor, patch, commits int
}

// parsePluginVersion reads a plugin version. A leading v and a trailing
// -dirty are ignored. Anything else, including the literal "dev" an untagged
// build reports and pre-release suffixes, is not comparable.
func parsePluginVersion(s string) (pluginVersion, bool) {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "v"), "-dirty")
	base, describe, dev := strings.Cut(s, "-")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return pluginVersion{}, false
	}
	var v pluginVersion
	for i, field := range []*int{&v.major, &v.minor, &v.patch} {
		n, ok := versionNumber(parts[i])
		if !ok {
			return pluginVersion{}, false
		}
		*field = n
	}
	if !dev {
		return v, true
	}
	count, hash, ok := strings.Cut(describe, "-g")
	n, numOK := versionNumber(count)
	if !ok || !numOK || !isLowerHex(hash) || len(hash) < 4 {
		return pluginVersion{}, false
	}
	v.commits = n
	return v, true
}

func versionNumber(s string) (int, bool) {
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func isLowerHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return s != ""
}

func (v pluginVersion) compare(other pluginVersion) int {
	return cmp.Or(
		cmp.Compare(v.major, other.major),
		cmp.Compare(v.minor, other.minor),
		cmp.Compare(v.patch, other.patch),
		cmp.Compare(v.commits, other.commits),
	)
}
