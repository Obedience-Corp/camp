package skills

import (
	"io/fs"
	"regexp"
	"strings"
)

// pluginBundleName is the shape of a bundle name a plugin may offer: one
// directory name under .campaign/skills/, never hidden.
var pluginBundleName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidPluginBundleName reports whether name is a bundle name a plugin may
// offer, and so a directory name camp may write or remove.
func ValidPluginBundleName(name string) bool {
	return pluginBundleName.MatchString(name)
}

// validBundleSegment reports whether slug is one plain directory name: not
// empty, no separator, not hidden, nothing that leaves the skills directory.
func validBundleSegment(slug string) bool {
	return fs.ValidPath(slug) && !strings.ContainsAny(slug, `/\:`) && !strings.HasPrefix(slug, ".")
}
