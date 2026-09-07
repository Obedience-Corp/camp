#!/usr/bin/env python3
"""PTY checks for `camp intent add` after the shared selector wiring.

Drives the compiled binary, inspects the rendered Type/Concept screens, and
checks that a saved intent lands on disk. Green unit tests cannot catch a
clipped help line or a missing filter row.

Run: just tui pty-intent-add
"""
import fcntl
import glob
import os
import pty
import select
import struct
import subprocess
import sys
import tempfile
import termios
import time

import pyte

ROWS, COLS = 32, 100
KEYS = {
    "ENTER": "\r",
    "DOWN": "\x1b[B",
    "UP": "\x1b[A",
    "ESC": "\x1b",
    "CTRLC": "\x03",
    "CTRLS": "\x13",
}
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 3.5


class Session:
    def __init__(self, binary, campaign, home):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.transcript = ""
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(campaign)
            os.execve(binary, [binary, "intent", "add", "--no-commit"], {
                "HOME": home,
                "PATH": os.path.dirname(binary) + ":" + os.environ.get("PATH", "/usr/bin:/bin"),
                "TERM": "xterm-256color",
                "LINES": str(ROWS),
                "COLUMNS": str(COLS),
                "NO_COLOR": "1",
            })
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.alive = self._drain(LAUNCH_BUDGET)
        self._snapshot("launch")

    def _drain(self, budget):
        deadline = time.time() + budget
        while time.time() < deadline:
            ready, _, _ = select.select([self.fd], [], [], 0.25)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError:
                return False
            if not data:
                return False
            self.stream.feed(data)
            self.transcript += data.decode("utf-8", "replace")
            deadline = time.time() + budget
        return True

    def _snapshot(self, label):
        self.snapshots.append((label, "\n".join(self.screen.display)))

    def press(self, *keys):
        for key in keys:
            if not self.alive:
                break
            os.write(self.fd, KEYS.get(key, key).encode())
            self.alive = self._drain(KEY_BUDGET)
            self._snapshot(f"key {key}")
        return self

    @property
    def display(self):
        return "\n".join(self.screen.display)

    def close(self):
        self._snapshot("final")
        try:
            os.write(self.fd, KEYS["CTRLC"].encode())
            time.sleep(0.2)
        except OSError:
            pass
        for closer in (lambda: os.close(self.fd), lambda: os.waitpid(self.pid, os.WNOHANG)):
            try:
                closer()
            except OSError:
                pass


FAILURES = []


def check(name, condition, detail=""):
    status = "PASS" if condition else "FAIL"
    print(f"[{status}] {name}" + (f"\n        {detail}" if detail and not condition else ""))
    if not condition:
        FAILURES.append(name)


def fixture(binary):
    home = tempfile.mkdtemp(prefix="camp-intent-pty-home-")
    campaign = os.path.join(home, "campaign")
    env = {
        "HOME": home,
        "PATH": os.path.dirname(os.path.abspath(binary)) + ":" + os.environ.get("PATH", "/usr/bin:/bin"),
        "GIT_AUTHOR_NAME": "Demo",
        "GIT_AUTHOR_EMAIL": "demo@example.com",
        "GIT_COMMITTER_NAME": "Demo",
        "GIT_COMMITTER_EMAIL": "demo@example.com",
    }
    subprocess.run(
        [binary, "init", campaign, "--name", "intent-pty",
         "--description", "pty", "--mission", "pty",
         "--no-register", "--no-skills"],
        env=env, check=True, capture_output=True,
    )
    for name in ("agent-simulator", "build-util", "camp"):
        os.makedirs(os.path.join(campaign, "projects", name), exist_ok=True)
    return campaign, home


def main():
    if len(sys.argv) != 2:
        print("usage: intent_add_pty.py <camp-binary>", file=sys.stderr)
        sys.exit(2)
    binary = os.path.abspath(sys.argv[1])
    campaign, home = fixture(binary)

    session = Session(binary, campaign, home)
    check("title: prompt renders", "intent" in session.display.lower() or "Title" in session.display, session.display)

    for ch in "pty-proof":
        session.press(ch)
    session.press("ENTER")
    type_step = session.display
    check("type: selector renders", "idea" in type_step, type_step)
    check("type: one help line", "type to filter" in type_step, type_step)
    check("type: no stacked picker help", type_step.count("esc cancel") <= 1, type_step)

    session.press("ENTER")
    concept = session.display
    check("concept type: none option", "(none)" in concept, concept)
    check("concept type: projects listed", "projects" in concept.lower(), concept)
    check("concept type: single help", concept.count("type to filter") == 1, concept)

    for ch in "proj":
        session.press(ch)
    filtered = session.display
    check("concept filter: query row", "filter: proj" in filtered, filtered)
    check("concept filter: projects remains", "projects" in filtered.lower(), filtered)

    session.press("ENTER")
    items = session.display
    check("concept items: camp is listed", "camp" in items, items)
    check("concept items: no leaked filter row from type query", "filter: proj" not in items, items)

    session.press("ENTER")
    session.press("CTRLS")
    session.close()

    inbox = glob.glob(os.path.join(campaign, ".campaign", "intents", "inbox", "*.md"))
    check("write: an intent file was saved", len(inbox) == 1, str(inbox))
    if inbox:
        body = open(inbox[0], encoding="utf-8").read()
        check("write: title is in the file", "pty-proof" in body, body)

    if FAILURES:
        print(f"\n{len(FAILURES)} failed")
        sys.exit(1)
    print("\nall intent-add pty checks passed")


if __name__ == "__main__":
    main()
