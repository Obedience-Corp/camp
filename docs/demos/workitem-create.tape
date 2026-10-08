# camp workitem create — the type comes from where you are.
#
# Created from workflow/explore it is an explore item; created from inside an
# existing explore item it lands beside it, never inside; created from the
# camp root it keeps the feature default. The marker is read back after the
# first create.
#
# Fixture + record (repo root, truecolor):
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/workitem-create-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/workitem-create.tape
Output out/workitem-create-raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 18
Set Width 1040
Set Height 620
Set Padding 22
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 45ms
Set Theme {"name":"Obedience Corp","background":"#0D0D11","foreground":"#F8F8F2","selection":"#3A2418","cursor":"#F2721C","black":"#0D0D11","red":"#FF5F57","green":"#50FA7B","yellow":"#FEBC2E","blue":"#F2721C","magenta":"#C2410C","cyan":"#FFB347","white":"#F8F8F2","brightBlack":"#A1A1AA","brightRed":"#FF5F57","brightGreen":"#50FA7B","brightYellow":"#FEBC2E","brightBlue":"#FFB347","brightMagenta":"#EA5513","brightCyan":"#FFB347","brightWhite":"#FFFFFF"}

# ── hidden setup: disposable HOME and camp seeded by the fixture ──
Hide
Type "export HOME=$(cd $CAMP_VHS_ROOT/home && pwd -P)" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export CAMP_THEME=dark" Enter
Type "export PS1='\[\e[38;2;242;114;28m\]\W \$\[\e[0m\] '" Enter
Type "cd $CAMP_VHS_ROOT/camp/workflow/explore && clear" Enter
Sleep 300ms
Show

Type "camp workitem create agent-chat-eval" Sleep 400ms Enter
Sleep 2s
Type "grep '^type:' agent-chat-eval/.workitem" Sleep 300ms Enter
Sleep 1500ms
Type "clear" Enter
Type "cd first-spike/notes && camp workitem create second-spike" Sleep 400ms Enter
Sleep 2500ms
Type "clear" Enter
Type "cd ../../../.. && camp workitem create tidy-notes" Sleep 400ms Enter
Sleep 3s
