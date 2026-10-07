#!/usr/bin/env python3
"""pty evidence for the camp notify browser.

Unit tests prove the model's transitions. They cannot prove the browser is
what a person sees, that d actually lands in .campaign/notices.yaml, or that
the non-interactive surfaces agree with it afterwards. This drives the real
binary in a terminal and asserts each of those separately:

  render    the live list, detail pane, help overlay, and empty state
  write     d and r change .campaign/notices.yaml, and dismissing one
            artifact root surfaces the next, because the browser re-runs the
            detectors instead of moving rows by hand
  readback  camp notify list and camp notify --json agree with the file
  handoff   y hands the bare fix command, without the dismiss hint, to the
            platform clipboard tool

It runs against a disposable fixture with stub clipboard tools, so it never
touches the operator's camp registry or clipboard.

Writes into <evidence-dir>:

    pty-transcript.txt      every snapshot, as pyte rendered it
    screen-snapshots.json   the same, with cursor positions
    pty-metadata.json       terminal and renderer details
    notices-after-*.yaml    the dismissal file after each session
    readback-*.txt|json     the non-interactive commands' output

Usage: notify_pty.py <fixture-dir> <evidence-dir>
"""
import fcntl
import hashlib
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

ROWS, COLS = 32, 120
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 2.0

# Pinned so the never-synced fix names a fixed source machine.
MACHINE = "fixture-mac"


def never_synced_id(root):
    """Mirror notice.SubjectID: the kind plus six hex digits of the root's hash."""
    return "never-synced-" + hashlib.sha256(root.encode()).hexdigest()[:6]


LEGACY = "dungeon-legacy-layout"
LINKS = "workitem-links-stale"
ROOT_A = never_synced_id("data/models")
ROOT_B = never_synced_id("data/zoo")
SYNC_FIX = "camp sync --from <id of %s> --artifacts-only" % MACHINE


def camp_env(fixture, color=False):
    env = {
        "HOME": os.path.join(fixture, "home"),
        "PATH": os.path.join(fixture, "bin") + ":" + os.environ.get("PATH", "/usr/bin:/bin"),
        "TERM": "xterm-256color",
        "LINES": str(ROWS),
        "COLUMNS": str(COLS),
        "CAMP_VHS_HANDOFF_LOG": os.path.join(fixture, "handoff.log"),
        "CAMP_MACHINE_NAME": MACHINE,
    }
    if not color:
        env["NO_COLOR"] = "1"
    return env


class Session:
    """One run of camp notify on a pty."""

    def __init__(self, fixture, camp, args, color=False, wait_for="Notices"):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.transcript = []
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(os.path.join(fixture, camp))
            binary = os.path.join(fixture, "bin", "camp")
            os.execve(binary, [binary] + args, camp_env(fixture, color))
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.wait_for(wait_for, LAUNCH_BUDGET)

    def drain(self, budget):
        deadline = time.time() + budget
        while time.time() < deadline:
            ready, _, _ = select.select([self.fd], [], [], 0.1)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError:
                return
            if not data:
                return
            self.stream.feed(data)

    def wait_for(self, needle, budget):
        """Drain until needle is on screen, then let the frame settle."""
        deadline = time.time() + budget
        while time.time() < deadline:
            self.drain(0.2)
            if self.has(needle):
                break
        self.drain(0.4)

    def press(self, keys, wait_for=None, budget=KEY_BUDGET):
        os.write(self.fd, keys)
        if wait_for:
            self.wait_for(wait_for, budget)
        else:
            self.drain(0.6)

    def has(self, needle):
        return any(needle in line for line in self.screen.display)

    def row_of(self, needle):
        for row, line in enumerate(self.screen.display):
            if needle in line:
                return row
        return -1

    def fg_of(self, needle):
        row = self.row_of(needle)
        if row < 0:
            return None
        return self.screen.buffer[row][self.screen.display[row].find(needle)].fg

    def snapshot(self, name):
        display = list(self.screen.display)
        self.snapshots.append({
            "name": name,
            "display": display,
            "cursor": {"x": self.screen.cursor.x, "y": self.screen.cursor.y},
        })
        self.transcript.append("===== %s =====" % name)
        self.transcript.extend(line.rstrip() for line in display)
        return display

    def wait_exit(self, budget=5.0):
        deadline = time.time() + budget
        while time.time() < deadline:
            self.drain(0.2)
            try:
                reaped, _ = os.waitpid(self.pid, os.WNOHANG)
            except ChildProcessError:
                return True
            if reaped:
                self.drain(0.2)
                return True
        return False

    def close(self):
        try:
            os.close(self.fd)
        except OSError:
            pass
        try:
            os.kill(self.pid, 9)
            os.waitpid(self.pid, 0)
        except (OSError, ChildProcessError):
            pass


def run_camp(fixture, args):
    return subprocess.run(
        [os.path.join(fixture, "bin", "camp")] + args,
        cwd=os.path.join(fixture, "camp"), env=camp_env(fixture),
        capture_output=True, text=True, timeout=60,
    )


def notices_file(fixture):
    path = os.path.join(fixture, "camp", ".campaign", "notices.yaml")
    if not os.path.exists(path):
        return ""
    with open(path) as fh:
        return fh.read()


def handed(fixture):
    with open(os.path.join(fixture, "handoff.log")) as fh:
        return [line.rstrip("\n") for line in fh if line.strip()]


class Run:
    def __init__(self, fixture, evidence):
        self.fixture, self.evidence = fixture, evidence
        self.failures, self.snapshots, self.transcript = [], [], []

    def check(self, ok, message):
        if not ok:
            self.failures.append(message)

    def keep(self, session):
        self.snapshots.extend(session.snapshots)
        self.transcript.extend(session.transcript)

    def save(self, name, text):
        # The fixture root is a host temp path; evidence names it by role so
        # the bundle passes the privacy scan and reads the same on any host.
        for root in {os.path.realpath(self.fixture), self.fixture}:
            text = text.replace(root, "$CAMP_VHS_ROOT")
        with open(os.path.join(self.evidence, name), "w") as fh:
            fh.write(text)
        self.transcript.append("===== file: %s =====" % name)
        self.transcript.extend(text.rstrip("\n").split("\n"))


def browse_copy_dismiss(run):
    s = Session(run.fixture, "camp", ["notify"])
    s.snapshot("01-open")
    run.check(s.has("LIVE (3)") and s.has("DISMISSED (0)"), "browser did not open on three live notices")
    run.check(s.has("this camp uses the visible dungeon/ layout"), "live list does not show the legacy-layout message")
    run.check(s.has("camp dungeon migrate") and s.has(LEGACY), "detail pane does not show the fix and id")

    s.press(b"y", wait_for="copied camp dungeon migrate")
    s.snapshot("02-copy-fix")
    run.check(s.has("copied camp dungeon migrate"), "y did not report the copy")

    s.press(b"j")
    s.press(b"j", wait_for=ROOT_A)
    s.snapshot("03-select-artifact-root")
    run.check(s.has("on another machine: " + SYNC_FIX), "detail pane does not show the artifact fix")
    run.check(s.has("subject   data/models"), "detail pane does not name the notice's subject")
    run.check(not s.has("(dismiss:"), "the dismiss hint leaked into the fix command")

    s.press(b"c", wait_for="copied " + SYNC_FIX)
    s.snapshot("04-copy-placeholder")
    run.check(s.has("fill in the <…> value and run it on another machine"),
              "copying a remote placeholder fix did not say what to fill in and where to run it")

    s.press(b"d", wait_for="dismissed " + ROOT_A + " (data/models)")
    s.snapshot("05-dismiss-root-a")
    run.check(s.has("DISMISSED (1)"), "d did not move the root to the dismissed list")
    run.check(s.has("data/models: declared artifact root"),
              "the dismissed root reads as a bare id instead of its subject and summary")
    run.check(s.has("data/zoo is a declared artifact root"),
              "dismissing the first root did not surface the second; the browser moved rows instead of re-detecting")

    s.press(b"k")
    s.press(b"k", wait_for="id        " + LEGACY)
    s.press(b"d", wait_for="dismissed " + LEGACY)
    s.snapshot("06-dismiss-legacy")
    run.check(s.has("LIVE (2)") and s.has("DISMISSED (2)"), "second dismissal did not land")

    s.press(b"r", wait_for="is not dismissed")
    s.snapshot("07-restore-refused-on-live")
    run.check(s.has(LINKS + " is not dismissed"), "r on a live row was not refused")

    s.press(b"?", wait_for="Notice keys")
    s.snapshot("08-help")
    run.check(s.has("dismiss the selected live notice") and s.has("copy the fix command"), "help overlay lacks the keys")
    s.press(b"\x1b", wait_for="LIVE (")
    s.snapshot("09-help-closed")
    run.check(s.has("LIVE (2)"), "esc in help did not return to the list")

    s.press(b"\r", wait_for="enter details")
    s.snapshot("10-detail-hidden")
    run.check(not s.has("camp workitem doctor --fix"), "enter did not hide the detail pane")

    s.press(b"q")
    exited = s.wait_exit()
    s.snapshot("11-after-quit")
    run.check(exited, "q did not end the browser")
    run.check(s.has("Dismissed " + ROOT_A + ". Undo: camp notify restore " + ROOT_A),
              "exit report does not name the root dismissal and its undo")
    run.check(s.has("Dismissed " + LEGACY + ". Undo: camp notify restore " + LEGACY),
              "exit report does not name the legacy dismissal and its undo")
    run.keep(s)
    s.close()

    stored = notices_file(run.fixture)
    run.save("notices-after-dismiss.yaml", stored)
    run.check(ROOT_A in stored and LEGACY in stored, "notices.yaml does not hold both dismissals")

    listing = run_camp(run.fixture, ["notify", "list"])
    run.save("readback-list-after-dismiss.txt", listing.stdout + listing.stderr)
    run.check(ROOT_A in listing.stdout and LEGACY in listing.stdout, "camp notify list disagrees with the browser")

    payload = run_camp(run.fixture, ["notify", "--json"])
    run.save("readback-json-after-dismiss.json", payload.stdout)
    doc = json.loads(payload.stdout)
    run.check([n["id"] for n in doc["live"]] == [LINKS, ROOT_B], "--json live list is %r" % doc["live"])
    run.check(sorted(d["id"] for d in doc["dismissed"]) == sorted([LEGACY, ROOT_A]), "--json dismissed list is %r" % doc["dismissed"])
    subjects = {d["id"]: d["subject"] for d in doc["dismissed"]}
    run.check(subjects.get(ROOT_A) == "data/models", "--json dismissed root lacks its subject: %r" % doc["dismissed"])
    run.check([n["subject"] for n in doc["live"]] == ["", "data/zoo"], "--json live subjects are %r" % doc["live"])

    plain = run_camp(run.fixture, ["notify"])
    run.save("readback-plain-after-dismiss.txt", plain.stdout + plain.stderr)
    run.check("LIVE NOTICES (2)" in plain.stdout and "DISMISSED NOTICES (2)" in plain.stdout,
              "bare camp notify off a terminal did not print the plain list")


def restore(run):
    s = Session(run.fixture, "camp", ["notify"])
    s.press(b"j")
    s.press(b"j", wait_for="id        " + LEGACY)
    s.snapshot("12-select-dismissed")
    s.press(b"r", wait_for="restored " + LEGACY)
    s.snapshot("13-restore-legacy")
    run.check(s.has("LIVE (3)") and s.has("DISMISSED (1)"), "r did not move the notice back to live")
    s.press(b"q")
    s.wait_exit()
    s.snapshot("14-after-restore-quit")
    run.check(s.has("Restored " + LEGACY + ". Undo: camp notify dismiss " + LEGACY), "exit report does not name the restore")
    run.keep(s)
    s.close()

    stored = notices_file(run.fixture)
    run.save("notices-after-restore.yaml", stored)
    run.check(LEGACY not in stored and ROOT_A in stored, "restore did not remove only the legacy dismissal")
    listing = run_camp(run.fixture, ["notify", "list"])
    run.save("readback-list-after-restore.txt", listing.stdout + listing.stderr)
    run.check(LEGACY not in listing.stdout and ROOT_A in listing.stdout, "camp notify list disagrees after restore")


def colors(run):
    s = Session(run.fixture, "camp", ["notify"], color=True)
    s.press(b"y", wait_for="copied ")
    s.snapshot("15-color-copied")
    ok_fg = s.fg_of("copied ")
    s.press(b"r", wait_for="is not dismissed")
    s.snapshot("16-color-refused")
    err_fg = s.fg_of("is not dismissed")
    run.check(ok_fg is not None and err_fg is not None and ok_fg != err_fg,
              "success and refusal render in the same color (%s vs %s)" % (ok_fg, err_fg))
    s.press(b"q")
    s.wait_exit()
    run.keep(s)
    s.close()


def empty(run):
    s = Session(run.fixture, "empty", ["notify"], wait_for="No notices")
    s.snapshot("17-empty")
    run.check(s.has("No notices.") and s.has("0 live · 0 dismissed"), "empty camp does not say no notices")
    s.press(b"q")
    s.wait_exit()
    run.keep(s)
    s.close()


def write_bundle(run):
    terminal = {
        "columns": COLS,
        "rows": ROWS,
        "pixel_width": 1200,
        "pixel_height": 700,
        "mode": "NO_COLOR except the color run; dark palette",
    }
    with open(os.path.join(run.evidence, "pty-transcript.txt"), "w") as fh:
        fh.write("\n".join(run.transcript) + "\n")
    with open(os.path.join(run.evidence, "screen-snapshots.json"), "w") as fh:
        json.dump({"renderer": "pyte", "terminal": terminal, "snapshots": run.snapshots}, fh, indent=2)
    with open(os.path.join(run.evidence, "pty-metadata.json"), "w") as fh:
        json.dump({
            "transport": "pty",
            "renderer": "pyte",
            "pyte_version": importlib.metadata.version("pyte"),
            "fake_home": True,
            "fixture_id": "camp-notify-v2",
            "terminal": terminal,
        }, fh, indent=2)


def main():
    fixture, evidence = sys.argv[1], sys.argv[2]
    os.makedirs(evidence, exist_ok=True)
    open(os.path.join(fixture, "handoff.log"), "w").close()
    run = Run(fixture, evidence)

    browse_copy_dismiss(run)
    restore(run)
    colors(run)
    empty(run)

    tool = "pbcopy" if sys.platform == "darwin" else ("wl-copy" if os.environ.get("WAYLAND_DISPLAY") else "xclip")
    log = handed(fixture)
    run.save("handoff.log", "\n".join(log) + "\n")
    want = ["%s camp dungeon migrate" % tool, "%s %s" % (tool, SYNC_FIX)]
    run.check(log[:2] == want, "clipboard was handed %r, want %r first" % (log, want))

    write_bundle(run)
    for failure in run.failures:
        print("FAIL: %s" % failure, file=sys.stderr)
    if run.failures:
        return 1
    print("notify: %d snapshots; dismiss, restore, copy, and readback verified" % len(run.snapshots))
    return 0


if __name__ == "__main__":
    sys.exit(main())
