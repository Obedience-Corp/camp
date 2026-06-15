# ID Schemes in camp

Three distinct ID formats exist. New code must use the workitem pattern.

## Intent IDs

Format: `<slug>-YYYYMMDD-HHMMSS`
Example: `add-dark-mode-toggle-20260119-153412`
Source: `internal/intent/slug.go:GenerateID`
Collision strategy: seconds-resolution timestamp; if two intents are captured
in the same second, the second create returns an error (collide-then-error).
The caller must retry.
Location on disk: `workflow/intents/<status>/<id>/` or `intents/<status>/<id>/`

## Quest IDs

Format: `qst_YYYYMMDD_<6-char-random-hex>`
Example: `qst_20260119_a3f9c2`
Source: `internal/quest/slug.go:GenerateID`
Collision strategy: crypto-random 6-character suffix; uniqueness is never
verified at create time (known gap, tracked).
Location on disk: `.campaign/quests/<id>/`

## Workitem IDs

Format: `<type>-<slug>-YYYY-MM-DD`
Example: `feature-add-dark-mode-2026-01-19`
Source: `internal/commands/workitem/create.go:169-208`
Collision strategy: full collision scan at create time; if a collision is
found, a random 6-character hex suffix is appended and the scan retries. This
is the most robust strategy and must be used for any new ID-generating code.
Location on disk: `workflow/<type>/<id>/`
WI-ref format: `WI-<sha256-of-id/6-chars>` (deterministic, re-rolls on
collision, source: `internal/workitem/ref.go`).

## Convergence Policy

New code generating IDs for persistent filesystem items must follow the
workitem pattern: generate a slug-based name, scan for collisions, append a
random hex suffix, and retry on collision. The intent seconds-collision error
and the quest unverified-unique pattern are known gaps documented here for
reference, not as patterns to copy.
