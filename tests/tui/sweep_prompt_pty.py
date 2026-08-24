#!/usr/bin/env python3
"""pty checks for the completed-run sweep prompt.

Green unit tests do not verify a TUI. A huh form can render with an option
missing, a clipped description, or no cursor while every test in the package
passes, so this drives the real compiled binary on a pty, renders its output
through a terminal emulator, and reads the screen a user would actually see.
It then checks the disk, because the render and the write are separate claims.

Six assertions:

  1. an explore item's question offers all six routing answers, so the
     route-to-docs answer cannot silently go missing
  2. a design with a completed run is reported as awaiting implementation
     evidence and is never offered as a question
  3. accepting the chore's Promote choice actually moves it into the dungeon
     (the directory on disk, not the word on the screen)
  4. acknowledging one run writes its ID and disappears from the next sweep
  5. marking an item recurring writes the durable policy visible in JSON
  6. Ctrl+C ends the run instead of re-asking about every remaining workitem

Run it through the justfile:  just tui sweep-prompt

Requires pyte. The justfile target provisions a throwaway venv for it.
"""
import fcntl
import os
import pty
import select
import struct
import subprocess
import sys
import termios
import time

import pyte

ROWS, COLS = 44, 130

KEYS = {"ENTER": "\r", "DOWN": "\x1b[B", "UP": "\x1b[A", "ESC": "\x1b",
        "CTRLC": "\x03", "LEFT": "\x1b[D", "RIGHT": "\x1b[C"}

# camp can stall around five seconds at bubbletea init on a mute pty (a known
# tty-query issue, fixed in fest and still present here). The launch budget
# absorbs it so a slow first paint is not misread as a hang.
LAUNCH_BUDGET = 9.0
KEY_BUDGET = 3.5


class Session:
    """One run of `camp workitem sweep --prompt` on a pty."""

    def __init__(self, campaign, home, binary):
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.snapshots = []
        self.transcript = ""
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.chdir(campaign)
            os.execve(binary, [binary, "workitem", "sweep", "--prompt"], {
                "HOME": home,
                "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
                "TERM": "xterm-256color",
                "LINES": str(ROWS),
                "COLUMNS": str(COLS),
                "NO_COLOR": "1",
            })
        # Both the ioctl and LINES/COLUMNS: a small default clips content and
        # sends you chasing a layout bug that does not exist.
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
        """Take the final snapshot before tearing down, never after."""
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
            except ChildProcessError:
                pass

    def dump(self):
        for label, body in self.snapshots:
            print(f"\n----- {label} -----")
            print(body.rstrip())


FAILURES = []


def check(name, condition, detail=""):
    status = "PASS" if condition else "FAIL"
    print(f"[{status}] {name}" + (f"\n        {detail}" if detail and not condition else ""))
    if not condition:
        FAILURES.append(name)


def fixture(binary):
    """Bootstrap a fresh fixture campaign and return (campaign, home)."""
    script = os.path.join(os.path.dirname(__file__), "..", "..",
                          "docs", "demos", "fixtures", "sweep-prompt-fixture.sh")
    env = dict(os.environ, CAMP_BIN=binary)
    out = subprocess.run(["bash", os.path.abspath(script)], env=env,
                         capture_output=True, text=True, check=True).stdout
    values = {}
    for line in out.splitlines():
        if line.startswith("export "):
            key, _, value = line[len("export "):].partition("=")
            values[key] = value
    return values["SWEEP_FIXTURE"], values["SWEEP_FIXTURE_HOME"]


def item_exists(campaign, rel):
    return os.path.isdir(os.path.join(campaign, rel))


def find_first_question(session, needle, limit=6):
    """Advance past questions until one whose text contains needle is on screen.

    The candidate order is discovery order, which is not something this check
    should pin. Declining a question is the neutral way to walk to the next one.
    """
    for _ in range(limit):
        if needle in session.display:
            return True
        if not session.alive:
            return False
        # Both selects default to the one-run acknowledgement; Enter therefore
        # leaves the item active while walking to the next question.
        session.press("ENTER")
    return needle in session.display


def check_explore_offers_every_routing_answer(binary):
    """1. The six-way routing select renders with every answer."""
    campaign, home = fixture(binary)
    session = Session(campaign, home, binary)
    found = find_first_question(session, "Where do the findings go?")
    rendered = session.display
    session.close()

    check("explore: the routing question renders", found, rendered)
    for answer in ("Route to docs", "Mark completed", "Someday", "Archive", "Keep active", "Recurring"):
        check(f"explore: the {answer!r} answer is on screen", answer in rendered, rendered)
    check("explore: the prompt names the directory it is asking about",
          "workflow/explore/provider-scan" in rendered, rendered)


def check_design_is_reported_not_asked(binary):
    """2. A design is reported as held, never offered as a question."""
    campaign, home = fixture(binary)
    session = Session(campaign, home, binary)
    launch = session.display
    session.close()

    check("design: the report names it as awaiting implementation evidence",
          "design awaits implementation evidence" in session.transcript, launch)
    check("design: it is never offered as a question",
          "workflow/design/observation" not in launch.split("Workflow run completed")[-1]
          or "Promote to completed?" not in launch,
          launch)
    check("design: it stays on disk",
          item_exists(campaign, "workflow/design/observation"))


def check_promote_confirm_moves_the_chore(binary):
    """3. Accepting Promote moves the directory, not just the screen."""
    campaign, home = fixture(binary)
    check("chore: it starts in workflow/", item_exists(campaign, "workflow/chore/tidy-imports"))

    session = Session(campaign, home, binary)
    found = find_first_question(session, "Promote to completed?")
    check("chore: the promote select renders", found, session.display)
    # The select defaults to Keep active (second row); move to Promote.
    session.press("UP", "ENTER")
    rendered = session.display
    session.close()

    check("chore: the screen reports the move",
          "workflow/chore/tidy-imports" in session.transcript, rendered)
    check("chore: the source directory is gone from workflow/",
          not item_exists(campaign, "workflow/chore/tidy-imports"),
          rendered)


def read_completion(campaign, home, binary, query):
    out = subprocess.run(
        [binary, "workitem", "--json", "--no-tokens", "--query", query],
        cwd=campaign,
        env={"HOME": home, "PATH": os.environ.get("PATH", "/usr/bin:/bin"), "NO_COLOR": "1"},
        capture_output=True, text=True, check=True,
    ).stdout
    import json
    return json.loads(out)["items"][0].get("completion", {})


def check_acknowledge_writes_and_suppresses(binary):
    """4. Keep active records the exact run and suppresses that candidate."""
    campaign, home = fixture(binary)
    session = Session(campaign, home, binary)
    found = find_first_question(session, "Where do the findings go?")
    check("acknowledge: explore question renders", found, session.display)
    session.press("ENTER")
    session.close()

    completion = read_completion(campaign, home, binary, "provider-scan")
    check("acknowledge: JSON reports review policy", completion.get("policy") == "review", str(completion))
    check("acknowledge: JSON reports the exact completed run",
          completion.get("reviewed_run_id") == "r1", str(completion))

    out = subprocess.run(
        [binary, "workitem", "sweep", "--json", "--dry-run"], cwd=campaign,
        env={"HOME": home, "PATH": os.environ.get("PATH", "/usr/bin:/bin"), "NO_COLOR": "1"},
        capture_output=True, text=True, check=True,
    ).stdout
    check("acknowledge: next sweep omits provider-scan", "provider-scan" not in out, out)


def check_recurring_writes_policy(binary):
    """5. Recurring is reachable and persists as workitem metadata."""
    campaign, home = fixture(binary)
    session = Session(campaign, home, binary)
    found = find_first_question(session, "Where do the findings go?")
    check("recurring: explore question renders", found, session.display)
    session.press("DOWN", "ENTER")
    session.close()

    completion = read_completion(campaign, home, binary, "provider-scan")
    check("recurring: JSON reports recurring policy",
          completion.get("policy") == "recurring", str(completion))
    marker = open(os.path.join(campaign, "workflow/explore/provider-scan/.workitem"), encoding="utf-8").read()
    check("recurring: marker stores the durable policy",
          "completion_policy: recurring" in marker, marker)


def check_ctrl_c_ends_the_run(binary):
    """6. Cancelling one question stops, rather than re-asking for each item."""
    campaign, home = fixture(binary)
    session = Session(campaign, home, binary)
    session.press("CTRLC")
    time.sleep(0.5)
    session._drain(1.5)
    transcript = session.transcript
    session.close()

    check("cancel: the run reports that it stopped",
          "cancelled" in transcript, transcript[-1500:])
    check("cancel: nothing was moved",
          item_exists(campaign, "workflow/chore/tidy-imports")
          and item_exists(campaign, "workflow/explore/provider-scan"),
          transcript[-1500:])


def main():
    binary = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "bin/camp")
    if not os.path.exists(binary):
        sys.exit(f"camp binary not found at {binary}; run `just build-camp` first")

    check_explore_offers_every_routing_answer(binary)
    check_design_is_reported_not_asked(binary)
    check_promote_confirm_moves_the_chore(binary)
    check_acknowledge_writes_and_suppresses(binary)
    check_recurring_writes_policy(binary)
    check_ctrl_c_ends_the_run(binary)

    print()
    if FAILURES:
        print(f"{len(FAILURES)} check(s) failed: " + ", ".join(FAILURES))
        sys.exit(1)
    print("all sweep prompt pty checks passed")


if __name__ == "__main__":
    main()
