## camp triage

Review the camp's workitems in a recorded session

### Synopsis

Review the camp's workitems in a recorded, resumable session.

A triage run freezes what the camp contains, collects evidence about each
item, records your verdicts, and applies them through camp's normal workitem
machinery. Every step is written to .campaign/triage/runs/<run-id>/, so a run
survives being interrupted and the decisions stay auditable afterwards.

Camp never calls a model. Agents read the queue and submit evidence and
proposals; you approve them; camp applies what you approved.

Session:
  start     Snapshot the camp and open a run
  status    Show where the active run stands
  abandon   Close the active run without applying it

Judgment:
  queue     List rows awaiting evidence or a proposal
  evidence  Submit a record, or print one with the known facts filled in
  propose   Propose a disposition for a row

The judgment commands are the driver seam: camp says what needs judging and
under what policy, anything you like does the judging, and camp validates what
comes back. A run leaves the judging phase only once every row holds evidence
and a proposal, or is explicitly marked judged without a record.

Examples:
  camp triage start                        Start a run over everything in scope
  camp triage start --scope type:design    Limit the run to design workitems
  camp triage status --json                Inspect the active run

  camp triage queue --json                 What still needs judging
  camp triage evidence template <id>       A record with camp's facts filled in
  camp triage evidence set <id> --file r.json
  camp triage propose <id> --disposition completed --summary "shipped in #239"

  camp triage abandon --reason "wrong scope"

```
camp triage [flags]
```

### Options

```
  -h, --help   help for triage
```

### Options inherited from parent commands

```
      --no-color   disable colored output
```

### SEE ALSO

* [camp](camp.md)	 - Manage your camps and the projects and festivals inside them
* [camp triage abandon](camp_triage_abandon.md)	 - Close the active triage run without applying it
* [camp triage apply](camp_triage_apply.md)	 - Execute the approved verdicts of the active run
* [camp triage approve](camp_triage_approve.md)	 - Record verdicts on proposed dispositions
* [camp triage evidence](camp_triage_evidence.md)	 - Submit or draft evidence for a row
* [camp triage init](camp_triage_init.md)	 - Scaffold .campaign/triage with the profile and guide
* [camp triage priorities](camp_triage_priorities.md)	 - Print the priorities brief for the active run
* [camp triage profile](camp_triage_profile.md)	 - Show the resolved triage profile
* [camp triage propose](camp_triage_propose.md)	 - Propose a disposition for a row
* [camp triage queue](camp_triage_queue.md)	 - List rows awaiting judgment
* [camp triage refresh](camp_triage_refresh.md)	 - Re-check the active run against the world
* [camp triage review](camp_triage_review.md)	 - Render the review documents for the active run
* [camp triage start](camp_triage_start.md)	 - Snapshot the camp and open a triage run
* [camp triage status](camp_triage_status.md)	 - Show where the active triage run stands
* [camp triage verify](camp_triage_verify.md)	 - Prove the camp matches the approved decisions
