# camp machine diagnose — a host that never resolved.
#
# The failure this records: when MagicDNS is down, every hop dies at
# `ssh: Could not resolve hostname ...: Name or service not known`, and diagnose
# answered with "camp unavailable (unreachable, or camp missing / too old for
# 'version --json')" — advice about the far machine, for a machine that was
# never addressable. Nothing on the far side was wrong.
#
# The recording shows two rows on purpose, because the point is that the new
# wording is targeted rather than a blanket replacement:
#
#   devbox      host resolves (an address literal), camp still unreachable
#               -> the generic "unavailable" message, which is still honest
#   mac-studio  MagicDNS name that does not resolve
#               -> RESOLVE ✗ quoting tailscale's own health text, and
#                  CAMP "not probed (the host does not resolve)"
#
# The unresolvable name is under an invented tailnet (example-net.ts.net), so
# the lookup genuinely fails through camp's real resolver path — nothing about
# the failure is faked. tailscale and ssh are fixture stubs so no live tailnet
# is contacted and no real device name is published.
#
# Fixture (see docs/demos/README.md):
#   FIXTURE=$(mktemp -d)
#   mkdir -p "$FIXTURE/bin" "$FIXTURE/home"
#   cp bin/camp "$FIXTURE/bin/camp"
#   cp docs/demos/fixtures/tailscale-dns-broken "$FIXTURE/bin/tailscale"
#   cp docs/demos/fixtures/ssh-unreachable "$FIXTURE/bin/ssh"
#   chmod +x "$FIXTURE/bin"/*
#   : > "$FIXTURE/machines.yaml"
#
# Record (repo root, truecolor):
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/machine-diagnose-dns.tape
Output out/machine-diagnose-dns-raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 14
Set Width 1320
Set Height 780
Set Padding 12
Set Margin 0
Set BorderRadius 0
Set Framerate 24
Set TypingSpeed 45ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

# ── hidden setup: fake HOME, fixture PATH, one reachable-name machine and one
#    whose MagicDNS name does not resolve ──
Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export FORCE_COLOR=1 CLICOLOR_FORCE=1" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export CAMP_MACHINES_PATH=$CAMP_VHS_ROOT/machines.yaml" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT" Enter
Type "camp machine add devbox --host 100.64.0.2 --auth ssh-agent --user builder" Enter
Sleep 500ms
Type "camp machine add mac-studio --host mac-studio.example-net.ts.net --auth tailscale-ssh --user builder" Enter
Sleep 500ms
Type "clear" Enter
Show

# ── the fleet: two machines, and nothing here says either one is unreachable ──
Type "camp machine list" Sleep 350ms Enter
Sleep 3000ms

# ── the hop failure an operator actually hits first ──
Type "clear" Enter
Sleep 200ms
Type "ssh -o BatchMode=yes builder@mac-studio.example-net.ts.net true" Sleep 350ms Enter
Sleep 3000ms

# ── diagnose: RESOLVE separates "never addressable" from "camp is missing" ──
Type "clear" Enter
Sleep 200ms
Type "camp machine diagnose" Sleep 350ms Enter
Sleep 9000ms

# ── the same finding as data: additive fields, existing consumers unaffected ──
Type "clear" Enter
Sleep 200ms
Type "camp machine diagnose mac-studio --json" Sleep 350ms Enter
Sleep 6000ms
