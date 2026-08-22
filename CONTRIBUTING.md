# Contributing

Contributions are welcome. By contributing you agree that your contribution is
licensed under the [Apache License 2.0](LICENSE), the same license as the
project.

## Developer Certificate of Origin

Every commit must be signed off:

```bash
git commit -s
```

The sign-off certifies the [Developer Certificate of Origin 1.1](https://developercertificate.org/):
that you wrote the change, or otherwise have the right to submit it under the
project's license. Pull requests with unsigned commits will be asked to rebase
with sign-offs before merge.

## Practical notes

- Run the repo gates before opening a PR: `just gate-fast` (release tags use `just gate`).
- Changing command help, flags, or adding CLI commands: run `just docs` and commit
  the regenerated `docs/cli-reference/`. `just docs-check` (wired into `gate-fast`
  and `gate`) fails when that generated reference is stale.
- Match the surrounding code's conventions; see the README for project layout.
