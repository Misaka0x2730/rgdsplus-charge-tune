# How it works

Notes on the RG DS Plus charging path and on what the app changes, from a
study of the stock firmware 20260915 (kernel 6.1.141, Rockchip BSP) and
measurements on a console. They are here so that anyone can check the app
does what it says.

## The charger

The RK3568 board charges through the **RK817** PMIC's built-in charger (I²C
bus 0, address 0x20); a **CW2015** fuel gauge reports the battery level. The
kernel's device tree describes the charger in
`/i2c@fdd40000/pmic@20/charger` (`compatible = "rk817,charger"`):

| Property | Stock | Meaning |
|---|---|---|
| `max_chrg_current` | 2000 | charge current, mA |
| `max_input_current` | 1500 | input current limit with a charger, mA |
| `max_chrg_voltage` | 4400 | charge voltage, mV (the app never changes it) |
| `min_input_voltage` | 4500 | input voltage below which the charger backs off, mV |
| `sample_res` | 10 | sense resistor, mΩ |

The BSP driver `drivers/power/supply/rk817_charger.c` reads them once, at
probe:

- The **charge current** goes to register 0xE4 bits [2:0], rounded down to a
  step: 500, 1000, 1500, 2000, 2500, 3000 or 3500 mA (the chip also has
  2750 mA, which the driver never selects). Nothing changes it later.
- The **input current limit** goes to register 0xE5 bits [2:0] on every
  plug-in, depending on what the USB port reports: `max_input_current` for a
  charger (DCP) or the DC input, 450 mA for a computer port (SDP), 1500 mA for
  a charging port (CDP). Steps: 80, 450, 850, 1500, 1750, 2000, 2500, 3000 mA.

The `usb`, `ac`, `charger` and `battery` power supplies in
`/sys/class/power_supply` have no writable properties, so the device tree is
the only supported place for these limits. The values the running kernel
uses are in `/proc/device-tree/i2c@fdd40000/pmic@20/charger/`.

The app allows at most 2500 mA charge current and 3000 mA input limit: the
battery (4247 mAh) has no temperature sensor in the device tree, so nothing
slows charging when it gets warm, and 2500 mA is about 0.6 C.

## What was measured

On a console at about 80 % with a 5 V charger and an FNB48S meter
(register values set at run time for the test):

| Input limit | Load | Battery current | From the charger |
|---|---|---|---|
| 1500 mA (stock) | idle | 954 mA | 1.5 A |
| 1500 mA (stock) | 4 cores busy | ~500 mA | 1.5 A |
| 3000 mA | 4 cores busy | ~1180 mA | — |
| 3000 mA | idle | ~1410 mA | 2.03 A |

With the stock limit the console under load takes the charging current from
the battery; with 3000 mA it draws more from the charger instead. A worn or
long cable drops enough voltage for the charger to reach `min_input_voltage`
and back off, which limits the current too.

## The boot partition

`/dev/block/by-name/boot` is `mmcblk1p3` on the system card, 64 MiB. It
holds a U-Boot FIT image with external data:

| Part | Offset | Size | Content |
|---|---|---|---|
| FIT header | 0 | 0x600 | device tree of the image: parts, sha256 hashes, configuration |
| `fdt` | 0x800 | 0x2DE4E | the kernel device tree |
| `kernel` | 0x2E800 | 0x25A2A00 | the kernel |
| `resource` | 0x25D1200 | 0x25AC00 | Rockchip resource image (RSCE): `rk-kernel.dtb` (a second copy of the device tree), boot logos, charging pictures |

U-Boot checks:

- the **sha256 of each part** (`/images/<part>/hash/value` in the header);
- the **SHA-1 of `rk-kernel.dtb`** in its resource entry
  (`hash_size` 20);
- **no signature**: the configuration's `signature` node has no `value`, and
  U-Boot's own device tree holds no public key.

U-Boot's code reads the kernel device tree as `rk-kernel.dtb` from the
resource image; the `fdt` part holds the same bytes. The app patches both,
so it does not matter which one ends up in the kernel. An image without
exactly these two copies is refused: a mode written into only one of them
could be reported as done and never take effect.

## The patch

For a mode the app changes, and nothing else:

1. `max_chrg_current` and `max_input_current` in **both** device tree copies
   (4 bytes each, big-endian);
2. the SHA-1 of `rk-kernel.dtb` in its resource entry;
3. the sha256 of the `fdt` and `resource` parts in the FIT header (the
   kernel's stays).

On the stock image these are the bytes at 0x154 (fdt hash), 0x304 (resource
hash), 0x749C and 0x74AC (input and charge current in `fdt`), 0x25D14E0
(resource entry hash), 0x25D969C and 0x25D96AC (the values in
`rk-kernel.dtb`). The app finds them by parsing, not from this list, so
another firmware with the same structure works as well; `task test:stock`
checks the list against a dump.

Patching back to 2000/1500 gives the stock image byte for byte.

Before writing, the app parses the new image again, verifies every hash
and checks that no byte outside those places changed. It writes only the
512-byte sectors that differ (5 sectors in 4 places), then reads the whole
partition back from the card and compares. A mismatch is written once more;
if it remains, or a write or read fails, the previous content is written
back and read back too. The error then says whether the partition is as it
was or, if not, which backup to restore and where the recovery steps are.

## Backups

One backup per firmware: before a write the app looks for a verified backup
with the same *base hash*, the sha256 of the image with the patched bytes
above zeroed. Images that differ only in the mode share it. With none (the
first run, or after a firmware update), the partition is saved as
`boot-<date>-<sha>.img` with a `.json` description (sha256, base hash,
firmware version, values). The file is written as `.part`, synced, read back
from the card and compared before it gets its name.
