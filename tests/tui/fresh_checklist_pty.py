#!/usr/bin/env python3
"""Drive camp fresh on the checklist fixture and record pyte snapshots.

The fixture script's own stdout contains a disposable home path, so this
script parses that privately and never writes it into the evidence files.
"""

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

COLS = 88
ROWS = 48
FIXTURE_ID = "camp-fresh-checklist-v1"


def fail(msg):
    print("FAIL: " + msg, file=sys.stderr)
    return 1


def run_fixture(binary):
    script = os.path.join(
        os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))),
        "docs",
        "demos",
        "fixtures",
        "fresh-checklist-fixture.sh",
    )
    env = os.environ.copy()
    env["CAMP_BIN"] = binary
    out = subprocess.check_output(["bash", script], env=env, text=True)
    values = {}
    for line in out.splitlines():
        if not line.startswith("export "):
            continue
        key, _, value = line[len("export ") :].partition("=")
        values[key] = value
    for key in ("FRESH_CHECKLIST_HOME", "FRESH_CHECKLIST_PROJECT"):
        if key not in values:
            raise SystemExit("fixture did not export " + key)
    return values


def drive(home, project, binary):
    import pyte

    screen = pyte.Screen(COLS, ROWS)
    stream = pyte.ByteStream(screen)
    pid, fd = pty.fork()
    if pid == 0:
        os.chdir(project)
        os.execve(
            binary,
            [binary, "fresh"],
            {
                "HOME": home,
                "PATH": os.path.dirname(binary) + ":/usr/bin:/bin",
                "TERM": "xterm-256color",
                "COLORTERM": "truecolor",
                "LINES": str(ROWS),
                "COLUMNS": str(COLS),
            },
        )
    fcntl_winsize(fd)
    raw = bytearray()
    initial = None
    deadline = time.time() + 40
    while time.time() < deadline:
        ready, _, _ = select.select([fd], [], [], 0.05)
        if not ready:
            status = os.waitpid(pid, os.WNOHANG)
            if status[0] == pid:
                break
            continue
        try:
            data = os.read(fd, 65536)
        except OSError:
            break
        if not data:
            break
        raw.extend(data)
        stream.feed(data)
        if initial is None and "running" in "\n".join(screen.display):
            initial = snapshot(screen, "initial")
    else:
        os.kill(pid, 15)
    try:
        os.waitpid(pid, 0)
    except ChildProcessError:
        pass
    return screen, raw, initial


def fcntl_winsize(fd):
    import fcntl

    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))


def snapshot(screen, name):
    display = [line.rstrip() for line in screen.display]
    return {
        "name": name,
        "display": display,
        "cursor": {"x": screen.cursor.x, "y": screen.cursor.y},
    }


def main():
    if len(sys.argv) != 3:
        print("usage: fresh_checklist_pty.py <camp-binary> <evidence-dir>", file=sys.stderr)
        return 2
    binary, evidence = sys.argv[1:]
    os.makedirs(evidence, exist_ok=True)
    values = run_fixture(os.path.abspath(binary))
    screen, raw, initial = drive(
        values["FRESH_CHECKLIST_HOME"], values["FRESH_CHECKLIST_PROJECT"], os.path.abspath(binary)
    )
    complete = snapshot(screen, "complete")
    text = "\n".join(complete["display"])
    problems = []
    if initial is None:
        initial = snapshot(screen, "initial")
        problems.append("never saw the follow-up spinner row")
    else:
        running = "\n".join(initial["display"])
        if "running" not in running:
            problems.append("initial frame lost the running row")
    if not any(frame.encode() in raw for frame in "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"):
        problems.append("spinner frames were not written")
    for needle in (
        "Sync",
        "Prune",
        "Follow-ups",
        "Work items",
        "already on it",
        "updated",
        "fix-leverage-worktrees",
        "install",
        "done",
        "not moved",
        "Fresh!",
    ):
        if needle not in text:
            problems.append("screen is missing %r" % needle)
    if "running" in text:
        problems.append("finished screen still says running")
    if "fix-l\n" in text or any(
        "fix-l" in line and "fix-leverage-worktrees" not in line for line in complete["display"]
    ):
        problems.append("branch name was split across a line")
    # A wrapped reason stays indented; the decision itself must be present whole.
    if "design awaits" not in text or "implementation evidence" not in text:
        problems.append("sweep reason was not rendered")

    terminal = {
        "columns": COLS,
        "rows": ROWS,
        "pixel_width": 980,
        "pixel_height": 680,
        "mode": "dark/adaptive truecolor",
    }
    transcript = ["===== initial =====", *initial["display"], "===== complete =====", *complete["display"]]
    with open(os.path.join(evidence, "pty-transcript.txt"), "w", encoding="utf-8") as fh:
        fh.write("\n".join(transcript) + "\n")
    with open(os.path.join(evidence, "screen-snapshots.json"), "w", encoding="utf-8") as fh:
        json.dump(
            {"renderer": "pyte", "terminal": {"columns": COLS, "rows": ROWS}, "snapshots": [initial, complete]},
            fh,
            indent=2,
        )
        fh.write("\n")
    with open(os.path.join(evidence, "pty-metadata.json"), "w", encoding="utf-8") as fh:
        json.dump(
            {
                "transport": "pty",
                "renderer": "pyte",
                "pyte_version": importlib.metadata.version("pyte"),
                "fake_home": True,
                "fixture_id": FIXTURE_ID,
                "terminal": terminal,
            },
            fh,
            indent=2,
        )
        fh.write("\n")
    for problem in problems:
        print("FAIL: " + problem, file=sys.stderr)
    if problems:
        print(text)
        return 1
    print("fresh-checklist: pyte rendered the checklist, prune names, follow-up, and sweep note")
    return 0


if __name__ == "__main__":
    sys.exit(main())
