// Package compat holds the campaign-era compatibility baseline: the frozen
// paths, markers, environment variables, keys, and grammars listed under
// "Frozen technical names" in docs/terminology.md.
//
// It carries no production code. The tests are the artifact, and they are
// deliberately literal: every expectation is written out as the exact string a
// shipped install already depends on, rather than compared against the constant
// that produces it, so renaming that constant fails here instead of passing.
//
// A failure in this package means a presentation change reached a contract. The
// fix is to revert the rename, not to update the expectation.
//
// Nothing here writes to the filesystem it runs on. Fixtures are read in place
// and parsers are fed bytes, so the baseline costs a developer's machine no
// mutation at all. The half of the baseline that needs a real workspace on real
// disk — discovery, load-and-save round trips, and the markers camp writes —
// runs against the binary in tests/integration/compat_oldstate_test.go.
package compat
