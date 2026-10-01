#!/bin/sh
# RG DS Plus Charge Tune launcher for the Anbernic RG DS Plus stock firmware
# (Ports menu). Installed as "Ports/RG DS Plus Charge Tune.sh" next to
# Ports/rgdsplus-charge-tune/ on TF1 or TF2.

APP_DIR="$(cd "$(dirname "$0")/rgdsplus-charge-tune" && pwd)" || exit 1
cd "$APP_DIR" || exit 1
mkdir -p data

export LD_LIBRARY_PATH="$APP_DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"

# Controller database: SDL reads extra mappings from this file if present.
if [ -f "$APP_DIR/data/gamecontrollerdb.txt" ]; then
    export SDL_GAMECONTROLLERCONFIG_FILE="$APP_DIR/data/gamecontrollerdb.txt"
fi

# Stock firmware input (same as DSFetch):
# * "dierct-keys-polled" is a keyboard that emits arrows and letters which
#   gabagool's keyboard map would turn into d-pad/A/L1/R2 presses on top of
#   the real gamepad, so keyboard input is off;
# * the firmware's SDL mapping names buttons by their labels (x:b3 is the top
#   button), so no Nintendo-style A/B X/Y swap is applied.
export DISABLE_KEYBOARD_INPUT="${DISABLE_KEYBOARD_INPUT:-1}"
export FLIP_FACE_BUTTONS="${FLIP_FACE_BUTTONS:-1}"

# SDL's built-in entry for "ANBERNIC-rk3568-keys" belongs to another model;
# the button numbers below come from the RG DS Plus device tree
# (gpio-keys-polled): A b0, B b1, Y b2, X b3, L1 b4, R1 b5, Select b6,
# Start b7, Menu b8, L3 b9, L2 b10, R2 b11, R3 b12; d-pad is hat 0.
export SDL_GAMECONTROLLERCONFIG="${SDL_GAMECONTROLLERCONFIG:-1900b655010000000100000000010000,ANBERNIC-rk3568-keys,a:b0,b:b1,y:b2,x:b3,leftshoulder:b4,rightshoulder:b5,back:b6,start:b7,guide:b8,leftstick:b9,lefttrigger:b10,righttrigger:b11,rightstick:b12,dpup:h0.1,dpdown:h0.4,dpleft:h0.8,dpright:h0.2,leftx:a0,lefty:a1,rightx:a2,righty:a3,platform:Linux,}"

# Device-specific tweaks (CHARGETUNE_ARGS="-screen 1", FLIP_FACE_BUTTONS=0,
# ...). See launcher.env.example.
if [ -f "$APP_DIR/data/launcher.env" ]; then
    . "$APP_DIR/data/launcher.env"
fi

# The app follows the system "swap screens" setting itself (-screen, then
# /sys/class/anbernic_misc/lcdswap); the firmware SDL's own swap would undo it.
unset SDL2_SWAP_LCD

chmod +x ./rgdsplus-charge-tune 2>/dev/null

# Keep the logs small: past 256 KB the current one becomes <name>.1 (the app
# rotates its own log).
for log in "$APP_DIR/data/launcher.log" "$APP_DIR/data/gabagool.log"; do
    [ -f "$log" ] || continue
    size=$(wc -c < "$log")
    # shellcheck disable=SC2086 # unquoted: some wc print leading spaces
    if [ $size -gt 262144 ]; then
        mv -f "$log" "$log.1"
    fi
done

# A reboot flag left over from a run that was killed must not reboot now.
rm -f "$APP_DIR/data/.reboot"

# stdout/stderr go to launcher.log (crash traces); the app log is
# rgdsplus-charge-tune.log.
# shellcheck disable=SC2086
./rgdsplus-charge-tune $CHARGETUNE_ARGS >> "$APP_DIR/data/launcher.log" 2>&1
status=$?
echo "rgdsplus-charge-tune exited with status $status at $(date)" >> "$APP_DIR/data/launcher.log"

sync
# The user chose "Reboot" after writing the boot partition: the app has
# closed its window and files, so reboot the way the system menu does.
if [ -f "$APP_DIR/data/.reboot" ]; then
    rm -f "$APP_DIR/data/.reboot"
    echo "rebooting at $(date)" >> "$APP_DIR/data/launcher.log"
    sync
    reboot
fi
exit 0
