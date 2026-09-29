# Captures docs/ficha-watch.webp. Run it through `make screenshots`, which
# builds ficha and the fixture first; vhs runs from the repo root.

Set Shell bash
Set FontFamily "DejaVu Sans Mono"
Set FontSize 22
# 80x58 cells in DejaVu Sans Mono at 22px. The stty below pins the size ficha
# lays out for, but a missing font falls back to a proportional one and garbles
# the capture, so the make target checks the font is installed first.
Set Width 1190
Set Height 1500
Set Padding 20
# A capture that never reaches the live header fails vhs, and with it make,
# before a broken frame can overwrite the committed image.
Set WaitTimeout 15s

Hide
# HOME points into the fixture so any path ficha prints reads as ~/work/webapp,
# and TZ is fixed so the clock times don't reveal the local timezone.
Type `export HOME="$PWD/bin/screenshots/fixture" CLAUDE_CONFIG_DIR="$PWD/bin/screenshots/fixture/config" TZ=UTC PATH="$PWD/bin:$PATH"`
Enter
Type "cd ~/work/webapp && stty cols 80 rows 58 && clear && ficha watch"
Enter
Wait+Screen /FOLLOWING/
Show
Sleep 500ms
Screenshot bin/screenshots/watch.png
Hide
Type "q"
