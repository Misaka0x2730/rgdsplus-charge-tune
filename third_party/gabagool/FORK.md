# gabagool fork

Copy of github.com/BrandonKowalski/gabagool/v2 **v2.24.0** (MIT, see
LICENSE), used through a `replace` directive in go.mod. It is the fork made
for [DSFetch](https://github.com/Misaka0x2730/dsfetch), with a few additions
for RG DS Plus Charge Tune (last section).

DSFetch changes:

* On-screen keyboard (`pkg/gabagool/keyboard.go`)
  * Y types a space (URL layout: digits/symbols); X cancels. Upstream used
    X for space and Y for cancel.
  * The footer lists every button (B delete, Y space, Select shift, X cancel,
    Start OK) instead of a lone "Menu Help".
  * `SetKeyboardLabels` translates the footer and help texts.
  * Cursor handling counts runes, not bytes: initial text with multi-byte
    characters (e.g. Cyrillic) no longer misplaces the cursor or panics on
    backspace.
* The NextUI fonts are removed (DSFetch does not use NextUI mode); the
  `nonextuifonts` stub is always compiled.
* Directional repeat (`internal/directional.go`): components used a 150 ms
  delay before the first repeat, measured from the last idle frame, so an
  ordinary d-pad tap often moved two rows. Now `RepeatDelay` (350 ms,
  counted from the press) and `RepeatInterval` (60 ms), adjustable with
  `SetDirectionalRepeat`.
* Lists: `MenuItem.Separator` draws a non-selectable divider (dimmed label and
  a line, opaque colours so it looks the same whatever was drawn before it)
  that navigation skips; `OnSelect` now also fires on d-pad moves, not only
  on L1/R1. An initial `SelectedIndex` outside the visible window is centred
  (window kept full) instead of landing on the last visible row.
* Text wrapping (`internal.WrapPieces`, used by `wrapTextToLines` and the
  selection message's height): lines also break between Chinese and Japanese
  characters, which have no spaces, instead of only at spaces. Closing
  punctuation (、。」, also a French » or ? after a space) never starts a
  line and opening punctuation (「 «) never ends one.
* Progress message (`ProcessMessage` with `ShowProgressBar`): the bar is placed
  below however many lines the message wraps into (`internal.ProgressLayout`),
  and message and bar are centred as one block. The fixed two-line layout put
  the third line of a long file name under the bar.

RG DS Plus Charge Tune changes:

* Lists: `MenuItem.Info` is a non-selectable line of text, drawn like an
  unfocused item without a highlight; navigation and multi-select skip it,
  as they skip separators (used for the values above the main menu).
  `ListOptions.ItemHeight` sets the row height (0 keeps 60): 47 fits the
  whole main screen (12 rows) without scrolling. The highlight's corner radius never
  exceeds half the row height.
* Text wrapping (`internal.WrapPieces`): a no-break space (U+00A0) no longer
  counts as a break point, so "5 V" stays on one line.
