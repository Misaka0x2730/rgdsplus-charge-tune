#!/bin/sh
# RG DS Plus Charge Tune entry for the stock frontend's Applications list
# (installed as "Roms/APPS/RG DS Plus Charge Tune.sh", icon in
# Roms/APPS/Imgs/). It starts the copy installed in Ports, so the program and
# its settings stay in one place; a copy in APPS on TF1 would be wiped by the
# Stock OS mod's updater, which empties /mnt/mmc/Roms/APPS.

name="RG DS Plus Charge Tune.sh"
card="$(cd "$(dirname "$0")/../.." 2>/dev/null && pwd)"
for ports in "$card/Ports" /mnt/sdcard/Ports /mnt/mmc/Ports; do
    if [ -f "$ports/$name" ] && [ -d "$ports/rgdsplus-charge-tune" ]; then
        exec sh "$ports/$name"
    fi
done
echo "RG DS Plus Charge Tune is not installed in Ports on TF1 or TF2" >&2
exit 1
