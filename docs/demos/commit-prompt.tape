# Build: just build quick-stable
# Record: just vhs record-color docs/demos/commit-prompt.tape
# Uses a disposable camp with isolated config and Git identity.
Output docs/demos/commit-prompt.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 17
Set Width 1200
Set Height 580
Set Padding 20
Set Framerate 20
Set TypingSpeed 60ms
Set Theme "Catppuccin Mocha"

Hide
Type "export PATH=$PWD/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export PS1='$ '" Enter
Type `eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/commit-prompt-fixture.sh)"` Enter
Sleep 5s
Type "cd ${COMMIT_PROMPT_FIXTURE:?fixture setup failed} || exit" Enter
Type "clear" Enter
Show

Type "camp commit" Enter
Sleep 3s
Type "Update community and release notes"
Sleep 4s
Enter
Sleep 3s
Type "git --no-pager log -1 --format=%s" Enter
Sleep 3s
