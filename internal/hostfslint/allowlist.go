package main

// defaultAllowlist is the CW0003-tests-01 ratchet: legacy host-FS test
// files still to migrate. Adding a NEW file to the violator set fails the
// gate. Removing a file as you migrate it tightens the rule.
//
// Helper-only files (no direct exec.Command("git")) belong here too once
// detected; allowlist-drained does not mean the package is clean.
var defaultAllowlist = []string{
	"./cmd/camp/project/remote/remote_test.go",
	"./internal/attach/attach_test.go",
	"./internal/doctor/checks/commits_test.go",
	"./internal/doctor/checks/head_test.go",
	"./internal/doctor/checks/integrity_test.go",
	"./internal/doctor/checks/lock_test.go",
	"./internal/doctor/checks/orphan_test.go",
	"./internal/doctor/checks/url_test.go",
	"./internal/doctor/checks/working_test.go",
	"./internal/git/branches_test.go",
	"./internal/git/checkout_test.go",
	"./internal/git/commit/commit_test.go",
	"./internal/git/commit_test.go",
	"./internal/git/executor_test.go",
	"./internal/git/info_exclude_test.go",
	"./internal/git/remote_test.go",
	"./internal/git/resolve_test.go",
	"./internal/git/retry_test.go",
	"./internal/git/run_test.go",
	"./internal/git/submodule_list_test.go",
	"./internal/git/submodule_orphan_test.go",
	"./internal/git/submodule_test.go",
	"./internal/leverage/author_resolver_test.go",
	"./internal/leverage/authors_test.go",
	"./internal/leverage/backfill_test.go",
	"./internal/leverage/blame_cache_test.go",
	"./internal/leverage/config_test.go",
	"./internal/leverage/projects_test.go",
	"./internal/leverage/sampler_test.go",
	"./internal/project/add_test.go",
	"./internal/project/list_test.go",
	"./internal/project/new_test.go",
	"./internal/project/remove_test.go",
	"./internal/project/resolve_test.go",
	"./internal/quest/autocommit_integration_test.go",
	"./internal/scaffold/init_behavior_test.go",
	"./pkg/commitkit/commitkit_test.go",
	"./tools/release-notes/main_test.go",
}
