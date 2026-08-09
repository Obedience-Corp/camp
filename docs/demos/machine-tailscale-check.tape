# camp machine — a Tailscale SSH machine that needs a browser approval.
#
# This is the failure the machine screen used to be unable to finish: the hop
# overlay clamped ssh's whole complaint to one line and cut the approval URL off
# after "https://l", and no key opened it.
#
# The tape shows both surfaces rendering the link whole on its own line, and
# both o (open) and c (copy) handing the exact URL to the platform. The ssh,
# opener, and clipboard are fixture stubs so nothing real is contacted and no
# live approval URL is published; camp's parse, render, and keybinding path is
# the real binary throughout.
#
# Fixture (see docs/demos/README.md):
#   FIXTURE=$(mktemp -d)
#   mkdir -p "$FIXTURE/bin" "$FIXTURE/home"
#   cp bin/camp "$FIXTURE/bin/camp"
#   cp docs/demos/fixtures/ssh-tailscale-check "$FIXTURE/bin/ssh"
#   for n in open xdg-open pbcopy xclip; do
#       cp docs/demos/fixtures/record-handoff "$FIXTURE/bin/$n"
#   done
#   chmod +x "$FIXTURE/bin"/*
#
# Record (repo root, truecolor):
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/machine-tailscale-check.tape
Output out/machine-tailscale-check-raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1000
Set Height 640
Set Padding 12
Set Margin 0
Set BorderRadius 0
Set Framerate 24
Set TypingSpeed 45ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

# ── hidden setup: fake HOME, fixture PATH, one tailscale-ssh machine ──
Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export FORCE_COLOR=1 CLICOLOR_FORCE=1" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export CAMP_MACHINES_PATH=$CAMP_VHS_ROOT/machines.yaml" Enter
Type "export CAMP_VHS_HANDOFF_LOG=$CAMP_VHS_ROOT/handoff.log" Enter
Type ": > $CAMP_VHS_HANDOFF_LOG" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT" Enter
Type "camp machine add devbox --host devbox.example-net.ts.net --auth tailscale-ssh --user builder" Enter
Sleep 800ms
Type "clear" Enter
Show

# ── the fleet screen ──
Type "camp machine" Sleep 350ms Enter
Sleep 3500ms

# select the tailscale-ssh machine (local is first)
Type "j" Sleep 1800ms

# t runs the connection test. The stub holds the connection open the way check
# mode does, so this waits out camp's own 10s deadline.
Type "t" Sleep 12000ms

# The detail pane: the approval URL, whole, on a line of its own.
Sleep 4000ms

# o hands it to the browser; the status line confirms the handoff.
Type "o" Sleep 3500ms

# c copies the same link for a terminal with no browser of its own.
Type "c" Sleep 3500ms

# ── the hop overlay: the surface that truncated the URL entirely ──
Type "q" Sleep 800ms
Type "clear" Enter
Sleep 400ms
Type "camp machine --path-output $CAMP_VHS_ROOT/hop.txt" Sleep 350ms Enter
Sleep 3500ms
Type "j" Sleep 1200ms
Enter Sleep 12000ms
Sleep 4500ms
Escape Sleep 800ms
Type "q" Sleep 800ms

# ── what camp actually handed the platform ──
Type "clear" Enter
Sleep 300ms
Type "cat handoff.log" Sleep 350ms Enter
Sleep 4000ms
