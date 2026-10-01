# Third-party software

RG DS Plus Charge Tune is built on the following projects. The release zip
carries their full license texts in `licenses/` (collected by
`scripts/collect-licenses.sh` from the exact module versions in `go.sum`).

## Code adapted into the app

| Project | License | Used for |
|---|---|---|
| [DSFetch](https://github.com/Misaka0x2730/dsfetch) — same author | MIT | Startup, screen router, sleep handling, launcher, i18n, build and CI layout, the gabagool fork |
| [Grout](https://github.com/rommapp/grout) — Copyright (c) 2025 Brandon T. Kowalski, Grout Contributors | MIT (`licenses/Grout-MIT.txt`) | Launcher and Docker build layout (through DSFetch) |

## Go modules linked into the binary

| Module | License |
|---|---|
| github.com/BrandonKowalski/gabagool/v2 (UI) v2.24.0, copied into `third_party/gabagool` with changes (see `third_party/gabagool/FORK.md`) | MIT |
| github.com/BrandonKowalski/certifiable | Unlicense (public domain) |
| github.com/veandco/go-sdl2 | BSD-3-Clause |
| golang.org/x/image, x/net, x/sys, x/text | BSD-3-Clause |
| github.com/nicksnyder/go-i18n/v2 | MIT |
| github.com/BurntSushi/toml | MIT |
| go.uber.org/atomic | MIT |
| github.com/holoplot/go-evdev | MIT |
| github.com/srwiley/oksvg, rasterx | BSD-3-Clause |

## Fonts

gabagool embeds **HackGen Console NF** (Copyright (c) 2019 Yuko OTAWARA;
SIL Open Font License 1.1), built from Hack (Copyright (c) 2018 Source
Foundry Authors; MIT), GenJyuu Gothic (Copyright (c) 2015 JIKASEI FONT KOUBOU;
OFL 1.1) and Nerd Fonts glyphs (Copyright (c) 2014 Ryan L McIntyre; OFL 1.1).
License texts: `licenses/HackGen-OFL-1.1.txt`, `licenses/HackGen-source-*.txt`.

## Not included

The app ships no part of the Anbernic firmware. The boot partition it
changes, and the backups it saves, stay on the console's cards; the tests
that use a dump of it (`task test:stock`) read it from a path you give.
