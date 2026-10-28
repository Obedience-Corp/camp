# Fresh interaction interface

`camp fresh [project] --interaction-json` and `camp fresh all --interaction-json`
run the ordinary Fresh workflow while an external UI presents its questions.
This is a transport mode, not a plan/apply reimplementation. Config, evidence,
choices, defaults, effects and persistence remain in Camp. The normal terminal
interface uses the same question definitions.

Keep stdin open for responses. Stdout emits one JSON object per line with
`version: 1` and one of these types:

- `ready`: the interface is connected, before Fresh starts work.
- `output`: `text` contains ordinary command output. Treat it as text.
- `prompt`: `prompt` contains `id`, `title`, `description`, `default`, and
  `options` (each has an opaque `value` and display `label`). Show all choices.
- `input_error`: `error` explains a rejected answer; the current question stays
  active so the UI can submit another answer.
- `complete`: the command finished; an optional `error` indicates failure.

Responses on stdin are newline-delimited JSON:

```json
{"id":"prompt-1","value":"yes"}
{"id":"prompt-2","cancel":true}
```

Only answer the currently active question. IDs are scoped to one process.
Unknown/stale IDs and values outside the offered choices are rejected before
any workflow action. Cancelling a question preserves that question's existing
CLI semantics: merged-item confirmation declines, completed-run cancellation
ends the sweep, and cancelling docs selection leaves that item in place.
Closing stdin cancels the command context; terminating the process stops Fresh.
Changes completed before cancellation are not rolled back.

The UI must wait for `ready`, reject unknown protocol versions, retain progress
and errors, and never silently fall back to a noninteractive run. A CLI without
this flag rejects it before running Fresh. Diagnostics before command execution
or after a failed completion may appear on stderr, and process exit status still
indicates command failure.

On a PTY, Camp enables raw input so replies are not echoed into the JSON stream.
Ordinary output is captured as `output` events; clients must not interpret it as
questions. Configured follow-up commands keep their existing stdin/EOF behavior.
The current question types cover merged-item confirmation, skip remaining,
completed-run disposition, and selecting an existing docs destination. Docs
moves still use the shared destination validation and move implementation.
