#!/usr/bin/env python3
"""PTY checks for `camp project list` recency + fuzzy overlay.

Launches the compiled command with a fixture campaign, types a filter, moves
selection, and verifies both the rendered screen and --path-output.

Run: just tui pty-project-list
"""
import fcntl
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

ROWS, COLS = 32, 120
KEYS = {
    "ENTER": "\r",
    "DOWN": "\x1b[B",
    "UP": "\x1b[A",
    "ESC": "\x1b",
    "CTRLC": "\x03",
}
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 3.5


class Session:
    def __init__(self, binary, campaign, home, extra_args):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(campaign)
            os.execve(binary, [binary, "project", "list"] + extra_args, {
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
    home = tempfile.mkdtemp(prefix="camp-plist-pty-home-")
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
        [binary, "init", campaign, "--name", "plist-pty",
         "--description", "pty", "--mission", "pty",
         "--no-register", "--no-skills"],
        env=env, check=True, capture_output=True,
    )
    for name, marker, content in (
        ("atlas", "go.mod", "module atlas\n"),
        ("grok-cli", "go.mod", "module grok-cli\n"),
        ("web", "package.json", "{}\n"),
        ("notes", "README.md", "notes\n"),
    ):
        path = os.path.join(campaign, "projects", name)
        os.makedirs(path, exist_ok=True)
        open(os.path.join(path, marker), "w", encoding="utf-8").write(content)
        subprocess.run(["git", "init", "-q"], cwd=path, check=True, capture_output=True)
        subprocess.run(["git", "add", "-A"], cwd=path, check=True, capture_output=True)
        subprocess.run(["git", "-c", "user.email=demo@example.com", "-c", "user.name=Demo",
                        "commit", "-qm", "initial"], cwd=path, check=True, capture_output=True)
    return campaign, home


def main():
    if len(sys.argv) != 2:
        print("usage: project_list_pty.py <camp-binary>", file=sys.stderr)
        sys.exit(2)
    binary = os.path.abspath(sys.argv[1])
    campaign, home = fixture(binary)
    out = os.path.join(home, "selected-path")

    session = Session(binary, campaign, home, ["--path-output", out])
    launch = session.display
    check("launch: atlas listed", "atlas" in launch, launch)
    check("launch: grok-cli listed", "grok-cli" in launch, launch)

    session.press("/")
    for ch in "go":
        session.press(ch)
    filtered = session.display
    check("filter go: overlay query", "go" in filtered, filtered)
    check("filter go: atlas still visible", "atlas" in filtered, filtered)
    check("filter go: grok-cli still visible", "grok-cli" in filtered, filtered)
    check("filter go: web dropped", "web" not in filtered or filtered.lower().count("web") == 0, filtered)

    session.press("DOWN")
    session.press("ENTER")
    session.close()

    check("write: path-output exists", os.path.isfile(out), out)
    if os.path.isfile(out):
        chosen = open(out, encoding="utf-8").read().strip()
        check("write: path-output is a fixture project",
              chosen.endswith("projects/atlas") or chosen.endswith("projects/grok-cli"),
              chosen)

    if FAILURES:
        print(f"\n{len(FAILURES)} failed")
        sys.exit(1)
    print("\nall project-list pty checks passed")


if __name__ == "__main__":
    main()
