# README demo. Regenerate from the repository root: vhs docs/assets/demo.tape

Output "docs/assets/demo.gif"

Set Shell "bash"
Set FontFamily "Fira Code"
Set FontSize 18
Set Width 1100
Set Height 680
Set Padding 28
Set Margin 0
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful
Set BorderRadius 10
Set TypingSpeed 45ms

Hide
Type `docs/assets/demo-setup.sh && D="${TMPDIR:-/tmp}/monodev-demo" && cd "$D/acme" && export PATH="$D/bin:$PATH" && unset MONODEV_ROOT && PS1='\[\e[1;36m\]acme\[\e[0m\] \[\e[35m\]❯\[\e[0m\] ' && clear`
Enter
Sleep 8s
Show

Type "git status --short"
Enter
Sleep 1.8s

Type "monodev checkout -n agent-context"
Enter
Sleep 1.2s
Type "monodev track --agents debug_helper.py"
Enter
Sleep 1.8s
Type "monodev save"
Enter
Sleep 2s

Type "clear"
Enter
Type "monodev unapply"
Enter
Sleep 1.2s
Type "ls -A"
Enter
Sleep 1s
Type "git status"
Enter
Sleep 2.2s

Type "monodev apply"
Enter
Sleep 1.2s
Type "ls -A"
Enter
Sleep 1s
Type "git status"
Enter
Sleep 4.5s
