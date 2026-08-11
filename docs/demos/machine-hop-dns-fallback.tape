# camp machine — hops survive a MagicDNS outage.
#
# The claim this records: when MagicDNS stops resolving, camp asks the local
# tailscaled for the peer's address and dials that, so the machine stays
# reachable while bare ssh-by-name keeps failing. The stub ssh answers ONLY at
# the peer address (100.72.165.77) — the name path cannot succeed — so every
# working camp command in this recording is proof the fallback dial engaged.
#
# Beats:
#   1  ssh by name           the outage: Name or service not known
#   2  camp machine diagnose RESOLVE leads with the address a hop will dial,
#                            names the peer table, quotes tailscale's health
#                            text — and CAMP shows a version probed OVER that
#                            fallback (the name dial cannot have produced it)
#   3  camp switch --shell-connect
#                            the hop line: stderr notice, and the eval'd ssh
#                            line dialing 100.72.165.77 with HostKeyAlias
#                            pinning the host key to the configured name
#   4  diagnose --json       dial_host / dial_via_peer as data
#   5  CAMP_NO_PEER_FALLBACK the escape hatch: camp fails the way ssh would
#
# The unresolvable name is under an invented tailnet (example-net.ts.net), so
# the lookup genuinely fails through camp's real resolver path. tailscale and
# ssh are fixture stubs so no live tailnet is contacted and no real device
# name is published.
#
# Fixture (see docs/demos/README.md):
#   FIXTURE=$(mktemp -d)   # short neutral path; --json prints socket paths
#   mkdir -p "$FIXTURE/bin" "$FIXTURE/home"
#   cp bin/camp "$FIXTURE/bin/camp"
#   cp docs/demos/fixtures/tailscale-dns-broken-peer "$FIXTURE/bin/tailscale"
#   cp docs/demos/fixtures/ssh-peer-fallback "$FIXTURE/bin/ssh"
#   chmod +x "$FIXTURE/bin"/*
#   : > "$FIXTURE/machines.yaml"
#
# Record (repo root, truecolor):
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/machine-hop-dns-fallback.tape
Output out/machine-hop-dns-fallback-raw.gif

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

# ── hidden setup: fake HOME, fixture PATH, one machine whose MagicDNS name
#    does not resolve but whose address the peer table still holds ──
Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export FORCE_COLOR=1 CLICOLOR_FORCE=1" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export CAMP_MACHINES_PATH=$CAMP_VHS_ROOT/machines.yaml" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT" Enter
Type "camp machine add mac-studio --host mac-studio.example-net.ts.net --auth tailscale-ssh --user builder" Enter
Sleep 500ms
Type "clear" Enter
Show

# ── the outage: by name, nothing works ──
Type "ssh -o BatchMode=yes builder@mac-studio.example-net.ts.net true" Sleep 350ms Enter
Sleep 3000ms

# ── diagnose: camp has an address anyway, and proved it by probing camp over it ──
Type "clear" Enter
Sleep 200ms
Type "camp machine diagnose mac-studio" Sleep 350ms Enter
Sleep 9000ms

# ── the hop: dim stderr notice, then an eval line that dials the peer address
#    with the host key pinned to the configured name ──
Type "clear" Enter
Sleep 200ms
Type "camp switch mac-studio:demo --shell-connect" Sleep 350ms Enter
Sleep 7000ms

# ── the same finding as data ──
Type "clear" Enter
Sleep 200ms
Type "camp machine diagnose mac-studio --json | grep -E 'dial|resolve'" Sleep 350ms Enter
Sleep 5000ms

# ── the escape hatch: opt out and camp fails exactly the way ssh did ──
Type "clear" Enter
Sleep 200ms
Type "CAMP_NO_PEER_FALLBACK=1 camp machine diagnose mac-studio" Sleep 350ms Enter
Sleep 8000ms
