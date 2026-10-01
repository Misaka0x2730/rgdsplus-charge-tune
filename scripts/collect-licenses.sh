#!/bin/sh
# collect-licenses.sh DEST
# Copies the license files of every Go module linked into the console build
# of rgdsplus-charge-tune (linux/arm64), plus Go's own license, into DEST.
# Binary releases must carry these texts (MIT/BSD notices).
set -eu
dest="${1:?usage: collect-licenses.sh DEST}"
cd "$(dirname "$0")/.."
mkdir -p "$dest"

# Go's runtime and standard library are linked in too. Homebrew keeps
# LICENSE one level above GOROOT.
goroot="$(go env GOROOT)"
for f in LICENSE PATENTS; do
    for d in "$goroot" "$goroot/.."; do
        if [ -f "$d/$f" ]; then
            cp "$d/$f" "$dest/go.$f"
            break
        fi
    done
done
[ -f "$dest/go.LICENSE" ] || { echo "collect-licenses: Go LICENSE not found" >&2; exit 1; }

GOOS=linux GOARCH=arm64 CGO_ENABLED=1 go list -deps \
    -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Dir}}{{end}}{{end}}' ./cmd/rgdsplus-charge-tune |
    sort -u |
    while read -r mod dir; do
        name=$(printf '%s' "$mod" | tr '/' '_')
        found=0
        for f in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
            [ -f "$f" ] || continue
            cp "$f" "$dest/$name.$(basename "$f")"
            found=1
        done
        if [ "$found" = 0 ]; then
            echo "collect-licenses: no license file in $mod ($dir)" >&2
            exit 1
        fi
    done

echo "collected $(ls "$dest" | wc -l | tr -d ' ') license files into $dest"
