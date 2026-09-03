#!/usr/bin/env python3
"""PTY assertions for zero-trust setup inside the machine TUI."""
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
                "CAMP_VHS_ROOT": fixture,
                "CAMP_MACHINES_PATH": os.path.join(fixture, "home", ".obey", "machines.yaml"),
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
    if "p set up access" not in denied:
        failures.append("detail pane does not offer in-TUI access setup")
    if "Could not reach it" in denied:
        failures.append("authentication failure is still presented as unreachable")

    session.press(b"p", 4.0)
    wizard = session.snapshot("zero-trust-wizard")
    for want in ["Set up secure access", "remote account password once", "never stores the password"]:
        if want not in wizard:
            failures.append("setup wizard missing %r" % want)

    session.press(b"\r", 3.0)
    password = session.snapshot("password-prompt")
    if "password:" not in password:
        failures.append("setup did not release the terminal for the password prompt")
    session.press(b"camp-demo-password\r", 4.0)
    preview = session.snapshot("pair-preview")
    if "Exchange keys with" not in preview or "Camp will not enable a login service" not in preview:
        failures.append("existing pair consent preview did not open after bootstrap")
    session.press(b"y", 1.0)
    session.press(b"\r", 6.0)
    paired = session.snapshot("paired-and-retested")
    if "reachable" not in paired:
        failures.append("machine TUI did not return and retest after pairing")

    with open(os.path.join(fixture, "home", ".obey", "machines.yaml")) as fh:
        machine_config = fh.read()
    if "identity_file:" not in machine_config or "camp_mac-studio_ed25519" not in machine_config:
        failures.append("dedicated identity was not persisted to machines.yaml")
    if not os.path.exists(os.path.join(fixture, "access-installed")):
        failures.append("password bootstrap did not install access")
    if not os.path.exists(os.path.join(fixture, "home", ".ssh", "authorized_keys")):
        failures.append("pair flow did not install the reverse key locally")

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
            "fixture_id": "camp-machine-zero-trust-bootstrap-v2",
            "terminal": terminal,
        }, fh, indent=2)
    session.close()

    for failure in failures:
        print("FAIL: %s" % failure, file=sys.stderr)
    if failures:
        return 1
    print("machine-auth-denied: in-TUI zero-trust bootstrap and pairing verified")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
