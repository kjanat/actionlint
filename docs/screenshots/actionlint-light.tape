#!/usr/bin/env -S sh -c 'cd "$(dirname "$0")" && exec vhs "$(basename "$0")"'
# Regenerate: make -C ../.. build && ./actionlint-light.tape (runnable from any directory)

Output actionlint-light.gif

Set Theme "AtomOneLight"
Set FontSize 15
Set Width 1240
Set Height 460
Set Padding 24

Hide
Type 'alias actionlint="$PWD/../../actionlint"'
Enter
Type 'clear'
Enter
Show

Sleep 500ms
Type 'actionlint -config-file actionlint.yaml demo-workflow.yaml'
Enter
Sleep 12s
