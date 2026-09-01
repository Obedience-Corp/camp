#!/usr/bin/env python3
"""PTY assertions for machine login denial and TUI-to-pair handoff."""
import fcntl
import importlib.metadata
import json
import os
import pty
import select
import struct
import sys
import termios
import time

import pyte

ROWS, COLS = 30, 110


class Session:
    def __init__(self, fixture):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.transcript = []
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            binary = os.path.join(fixture, "bin", "camp")
            os.chdir(fixture)
            os.execve(binary, [binary, "machine"], {
                "HOME": os.path.join(fixture, "home"),
                "PATH": os.path.join(fixture, "bin") + ":" + os.environ.get("PATH", "/usr/bin:/bin"),
                "TERM": "xterm-256color",
                "LINES": str(ROWS),
                "COLUMNS": str(COLS),
                "NO_COLOR": "1",
                "CAMP_MACHINES_PATH": os.path.join(fixture, "machines.yaml"),
            })
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.drain(8.0)

    def drain(self, budget):
        deadline = time.time() + budget
        while time.time() < deadline:
            ready, _, _ = select.select([self.fd], [], [], 0.2)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError:
                return
            if not data:
                return
            self.stream.feed(data)

    def press(self, keys, budget=2.0):
        os.write(self.fd, keys)
        self.drain(budget)

    def snapshot(self, name):
        display = list(self.screen.display)
        self.snapshots.append({
            "name": name,
            "display": display,
            "cursor": {"x": self.screen.cursor.x, "y": self.screen.cursor.y},
        })
        self.transcript.append("===== %s =====" % name)
        self.transcript.extend(line.rstrip() for line in display)
        return "\n".join(display)

    def close(self):
        try:
            os.close(self.fd)
        except OSError:
            pass
        try:
            os.kill(self.pid, 9)
        except OSError:
            return
        try:
            os.waitpid(self.pid, 0)
        except OSError:
            pass


def main():
    fixture, evidence = sys.argv[1], sys.argv[2]
    os.makedirs(evidence, exist_ok=True)
    failures = []
    session = Session(fixture)

    session.press(b"\x1b[B")
    session.press(b"t", 4.0)
    denied = session.snapshot("auth-denied")
    if "SSH login denied" not in denied:
        failures.append("detail pane does not distinguish SSH login denial")
    if "camp machine pair <this-machine>" not in denied:
        failures.append("detail pane does not name the pair command to run on the peer")
    if "p pair · e edit login/key" in denied:
        failures.append("denied-login pane advertised a pair start that cannot succeed")
    if "Could not reach it" in denied:
        failures.append("authentication failure is still presented as unreachable")

    session.press(b"p", 4.0)
    stayed = session.snapshot("pair-stays-in-tui")
    if "SSH login denied" not in stayed:
        failures.append("p left the TUI instead of keeping the denied-login pane")
    if "On mac-studio, run: camp machine pair <this-machine>" not in stayed:
        failures.append("p did not name the pair command to run on the peer")
    if "pair must run from a machine that can already reach mac-studio" in stayed:
        failures.append("p handed off to a doomed pair hop")

    terminal = {
        "columns": COLS,
        "rows": ROWS,
        "pixel_width": 1000,
        "pixel_height": 640,
        "mode": "dark/adaptive truecolor",
    }
    with open(os.path.join(evidence, "pty-transcript.txt"), "w") as fh:
        fh.write("\n".join(session.transcript) + "\n")
    with open(os.path.join(evidence, "screen-snapshots.json"), "w") as fh:
        json.dump({"renderer": "pyte", "terminal": terminal, "snapshots": session.snapshots}, fh, indent=2)
    with open(os.path.join(evidence, "pty-metadata.json"), "w") as fh:
        json.dump({
            "transport": "pty",
            "renderer": "pyte",
            "pyte_version": importlib.metadata.version("pyte"),
            "fake_home": True,
            "fixture_id": "camp-machine-auth-denied-v1",
            "terminal": terminal,
        }, fh, indent=2)
    session.close()

    for failure in failures:
        print("FAIL: %s" % failure, file=sys.stderr)
    if failures:
        return 1
    print("machine-auth-denied: login state and in-TUI pair guidance verified")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
