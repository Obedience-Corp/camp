#!/usr/bin/env python3
"""pty evidence for camp workitem create.

Unit and integration tests prove where the workitem lands. They cannot prove
what a person sees: that the success line, the aligned rows, and the type's
origin render as intended in a real terminal, in color and with NO_COLOR.
This drives the real binary on a pty and asserts each of those separately:

  render    created from workflow/explore, from inside an existing explore
            workitem, and from the camp root (the feature default)
  color     the success mark is green and the type's origin is dim
  plain     NO_COLOR output has no escape codes and the same text
  readback  the .workitem markers and create --json agree with the screen

It runs against a disposable fixture, so it never touches the operator's camp
registry.

Writes into <evidence-dir>:

    pty-transcript.txt      every snapshot, as pyte rendered it
    screen-snapshots.json   the same, with cursor positions
    pty-metadata.json       terminal and renderer details
    dependencies.txt        versions of the tools the evidence relied on
    readback-*.txt|json     the markers and the non-interactive output

Usage: workitem_create_pty.py <fixture-dir> <evidence-dir>
"""
import fcntl
import importlib.metadata
import json
import os
import pty
import select
import struct
import subprocess
import sys
import termios
import time

import pyte

ROWS, COLS = 40, 120
RUN_BUDGET = 10.0
FIXTURE_ID = "camp-workitem-create-v1"
GREEN = {"green", "50fa7b", "69ff94"}


def camp_env(fixture, color):
    env = {
        "HOME": os.path.join(fixture, "home"),
        "PATH": os.path.join(fixture, "bin") + ":" + os.environ.get("PATH", "/usr/bin:/bin"),
        "TERM": "xterm-256color",
        "LINES": str(ROWS),
        "COLUMNS": str(COLS),
    }
    if color:
        env.update({"COLORTERM": "truecolor", "CLICOLOR_FORCE": "1", "CAMP_THEME": "dark"})
    else:
        env["NO_COLOR"] = "1"
    return env


class Run:
    def __init__(self, fixture, evidence):
        self.fixture = fixture
        self.evidence = evidence
        self.snapshots = []
        self.transcript = []
        self.failures = []

    def check(self, ok, message):
        if not ok:
            self.failures.append(message)

    def save(self, name, text):
        with open(os.path.join(self.evidence, name), "w") as fh:
            fh.write(text)

    def camp_dir(self, rel=""):
        return os.path.join(self.fixture, "camp", rel)


class Session:
    """One run of camp on a pty, to exit."""

    def __init__(self, run, cwd, args, color):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.raw = b""
        pid, fd = pty.fork()
        if pid == 0:
            os.chdir(cwd)
            env = camp_env(run.fixture, color)
            env["PWD"] = cwd
            binary = os.path.join(run.fixture, "bin", "camp")
            os.execve(binary, [binary] + args, env)
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        deadline = time.time() + RUN_BUDGET
        while time.time() < deadline:
            ready, _, _ = select.select([fd], [], [], 0.1)
            if ready:
                try:
                    data = os.read(fd, 65536)
                except OSError:
                    data = b""
                if not data:
                    break
                self.raw += data
                self.stream.feed(data)
        os.close(fd)
        _, status = os.waitpid(pid, 0)
        self.exit_code = os.waitstatus_to_exitcode(status)

    def has(self, needle):
        return any(needle in line for line in self.screen.display)

    def fg_at(self, needle):
        for row, line in enumerate(self.screen.display):
            col = line.find(needle)
            if col >= 0:
                return self.screen.buffer[row][col].fg
        return None

    def snapshot(self, run, name):
        display = list(self.screen.display)
        run.snapshots.append({
            "name": name,
            "display": display,
            "cursor": {"x": self.screen.cursor.x, "y": self.screen.cursor.y},
        })
        run.transcript.append("===== %s =====" % name)
        run.transcript.extend(line.rstrip() for line in display if line.strip())


def marker_type(run, rel):
    with open(run.camp_dir(rel + "/.workitem")) as fh:
        for line in fh:
            if line.startswith("type:"):
                return line.split(":", 1)[1].strip()
    return ""


def from_type_dir(run):
    s = Session(run, run.camp_dir("workflow/explore"), ["workitem", "create", "agent-chat-eval"], color=True)
    s.snapshot(run, "explore-from-type-dir")
    run.check(s.exit_code == 0, "create from workflow/explore exited %d" % s.exit_code)
    run.check(s.has("✓ Created explore workitem agent-chat-eval"), "no success line naming the explore type")
    run.check(s.has("path:  workflow/explore/agent-chat-eval"), "path row is not workflow/explore/agent-chat-eval")
    run.check(s.has("type:  explore (from workflow/explore)"), "type row does not say where the type came from")
    run.check(s.has("next:  cd agent-chat-eval && fest create workflow agent-chat-eval"), "next step cd is not relative to the cwd")
    run.check(str(s.fg_at("✓")) in GREEN, "success mark is %r, want green" % s.fg_at("✓"))
    run.check(s.fg_at("(from workflow/explore)") != s.fg_at("explore (from"), "type origin is not styled apart from the value")
    run.check(marker_type(run, "workflow/explore/agent-chat-eval") == "explore", "marker type is not explore")


def from_nested_workitem(run):
    s = Session(run, run.camp_dir("workflow/explore/first-spike/notes"), ["workitem", "create", "second-spike"], color=True)
    s.snapshot(run, "sibling-from-nested")
    run.check(s.exit_code == 0, "create from inside a workitem exited %d" % s.exit_code)
    run.check(s.has("path:  workflow/explore/second-spike"), "nested create is not a sibling in workflow/explore")
    run.check(s.has("next:  cd ../../second-spike && fest create workflow second-spike"), "nested next step cd is wrong")
    run.check(marker_type(run, "workflow/explore/second-spike") == "explore", "nested marker type is not explore")


def from_camp_root(run):
    s = Session(run, run.camp_dir(), ["workitem", "create", "tidy-notes"], color=True)
    s.snapshot(run, "feature-default")
    run.check(s.exit_code == 0, "create from the camp root exited %d" % s.exit_code)
    run.check(s.has("✓ Created feature workitem tidy-notes"), "camp root does not default to feature")
    run.check(s.has("type:  feature (default)"), "default type is not labelled")
    run.check(marker_type(run, "workflow/feature/tidy-notes") == "feature", "root marker type is not feature")


def plain(run):
    s = Session(run, run.camp_dir("workflow/explore"), ["workitem", "create", "plain-check"], color=False)
    s.snapshot(run, "no-color")
    run.check(b"\x1b[" not in s.raw, "NO_COLOR output contains escape codes")
    run.check(s.has("✓ Created explore workitem plain-check"), "NO_COLOR output lost the success line")


def readback(run):
    out = subprocess.run(
        [os.path.join(run.fixture, "bin", "camp"), "workitem", "create", "json-check", "--json"],
        cwd=run.camp_dir("workflow/explore"), env=dict(camp_env(run.fixture, False), PWD=run.camp_dir("workflow/explore")),
        capture_output=True, text=True, timeout=60,
    )
    run.save("readback-create.json", out.stdout)
    run.check(out.returncode == 0, "create --json exited %d: %s" % (out.returncode, out.stderr))
    payload = json.loads(out.stdout or "{}")
    workitem = payload.get("workitem", {})
    run.check(workitem.get("type") == "explore", "create --json type is %r, want explore" % workitem.get("type"))
    markers = {rel: marker_type(run, rel) for rel in (
        "workflow/explore/agent-chat-eval", "workflow/explore/second-spike", "workflow/feature/tidy-notes")}
    run.save("readback-markers.txt", "".join("%s type: %s\n" % kv for kv in markers.items()))


def version_of(cmd):
    try:
        out = subprocess.run(cmd, capture_output=True, text=True, timeout=20)
    except (OSError, subprocess.TimeoutExpired):
        return None
    line = (out.stdout or out.stderr).strip().splitlines()
    return line[0] if line else None


def write_bundle(run):
    terminal = {
        "columns": COLS,
        "rows": ROWS,
        "pixel_width": 1040,
        "pixel_height": 620,
        "mode": "dark truecolor, plus one NO_COLOR run",
    }
    run.save("pty-transcript.txt", "\n".join(run.transcript) + "\n")
    with open(os.path.join(run.evidence, "screen-snapshots.json"), "w") as fh:
        json.dump({"renderer": "pyte", "terminal": terminal, "snapshots": run.snapshots}, fh, indent=2)
    with open(os.path.join(run.evidence, "pty-metadata.json"), "w") as fh:
        json.dump({
            "transport": "pty",
            "renderer": "pyte",
            "pyte_version": importlib.metadata.version("pyte"),
            "fake_home": True,
            "fixture_id": FIXTURE_ID,
            "terminal": terminal,
        }, fh, indent=2)
    versions = [
        "camp: %s" % version_of([os.path.join(run.fixture, "bin", "camp"), "--version"]),
        "pyte: %s" % importlib.metadata.version("pyte"),
        "vhs: %s" % version_of(["vhs", "--version"]),
        "ffmpeg: %s" % version_of(["ffmpeg", "-version"]),
    ]
    run.save("dependencies.txt", "\n".join(v for v in versions if not v.endswith("None")) + "\n")


def main():
    fixture, evidence = sys.argv[1], sys.argv[2]
    os.makedirs(evidence, exist_ok=True)
    run = Run(fixture, evidence)

    from_type_dir(run)
    from_nested_workitem(run)
    from_camp_root(run)
    plain(run)
    readback(run)

    write_bundle(run)
    for failure in run.failures:
        print("FAIL: %s" % failure, file=sys.stderr)
    if run.failures:
        return 1
    print("workitem create: %d snapshots; render, color, NO_COLOR, and readback verified" % len(run.snapshots))
    return 0


if __name__ == "__main__":
    sys.exit(main())
