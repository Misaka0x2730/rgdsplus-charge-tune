#!/bin/sh
# check-links.sh FILE.md...
# Fails when a Markdown file links to a local file that is not there:
# [text](path) and <img src="path"> relative to the file's folder. Web links,
# mail links and #anchors are skipped. `task package` runs it on the README
# that goes into the zip.
set -eu
status=0
for md in "$@"; do
    dir=$(dirname "$md")
    links=$(grep -oE '\]\([^)]+\)|src="[^"]+"' "$md" |
        sed -E 's/^\]\((.*)\)$/\1/; s/^src="(.*)"$/\1/' || true)
    for link in $links; do
        case "$link" in
        http://* | https://* | mailto:* | \#*) continue ;;
        esac
        target="$dir/${link%%#*}"
        if [ ! -e "$target" ]; then
            echo "check-links: $md links to $link, which is missing" >&2
            status=1
        fi
    done
done
exit $status
