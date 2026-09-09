# Git keyboard workspace demo (alt-screen). The pre-overhaul sequence
# (treehouse, then immediately treehouse status as shell input) is invalid:
# treehouse now owns the terminal until q or a shell is opened. Intended
# frame: two local branches, details panel, English Enter resume line.
# vhs was unavailable when this tape was rewritten; render locally with vhs.

Output demo.gif

Require treehouse
Require git

Set Shell "bash"
Set FontSize 20
Set Width 900
Set Height 600
Set Padding 20
Set Theme "Catppuccin Mocha"

Env TREEHOUSE_NO_UPDATE_CHECK "1"
Env SHELL "/bin/bash"

Hide
Type "TMPDIR=$(mktemp -d) && git init --bare -b main $TMPDIR/remote.git && git init -b main $TMPDIR/myproject && cd $TMPDIR/myproject && git remote add origin $TMPDIR/remote.git && git commit --allow-empty -m 'init' && git branch feature/alpha && git branch feature/beta && git push origin main && clear && cd $TMPDIR"
Enter
Sleep 2s
Show

Type "cd myproject"
Enter
Sleep 1s

Type "treehouse"
Enter
Sleep 3s
Type "q"
Sleep 1s
