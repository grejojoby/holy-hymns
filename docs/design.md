# Holy Hymns mobile design

## Reference and direction

The selected reference is [Hymn Mobile App by Wegba Eminokanju](https://dribbble.com/shots/25755054-Hymn-Mobile-App), particularly its plain, searchable hymn list. Holy Hymns uses that direction for a reading-focused catalogue: a clear title, prominent search, short filter controls, full-width song rows and a persistent three-tab navigation bar. No artwork, screenshots, icons or other assets were copied from the reference.

The catalogue keeps search and song titles prominent. Categories and the Latin/Malayalam alphabet open on demand. Active filters are named and can be cleared. Recent and featured views use the same list. The lyric reader gives space to the words, with a back action, saved state, available-script selector and text-size controls.

Administration uses the same controls and typography. Its entry appears only in the account screen for authorized administrators and owners. Draft status, review warnings, publishing and destructive actions remain explicit.

## Typography and layout

- English and Manglish use the platform system font. Malayalam uses the bundled Noto Sans Malayalam Regular font, including Malayalam titles and script controls.
- Lyrics start at 20 logical pixels and can be adjusted from 18 to 40 in steps of 2. Malayalam line height is 1.75 times the chosen size; Manglish is 1.65 times. Preserve source stanza breaks.
- Catalogue titles use 16 logical pixels; secondary titles use 13. Main screen titles use 20 and lyric titles use 22. Search and shared body text use 14; body line height is 22. Catalogue controls use 13.
- Text follows system accessibility scaling. The scale and spacing were reduced after reviewing the user’s native screenshots to fit more content on screen; touch areas remain generous. Containers should grow and rows of controls should wrap rather than clip at enlarged text sizes. Do not truncate lyrics or force song titles onto one line.
- Pages use 16-pixel side padding and a single column capped at 680 pixels. The reader is capped at 640 pixels. Safe-area insets protect content from device cutouts and system navigation.
- Interactive controls target at least 48 × 48 logical pixels; bottom tabs have at least 52 pixels of height and song rows at least 64. A visual switch can be smaller only when its actual touch area meets the control target.

## Color and contrast

The palette uses neutral paper/surfaces, dark text and a restrained green accent. Appearance follows the device by default, with explicit light and dark options. Errors include explanatory text, and selection uses accessible selected state as well as visual styling.

Contrast was calculated from the sRGB theme values using relative luminance and `(lighter + 0.05) / (darker + 0.05)`. The following ratios cover enabled text; disabled control opacity is excluded.

| Pair | Light | Dark |
| --- | ---: | ---: |
| Primary text / paper | 14.16:1 | 15.86:1 |
| Secondary text / paper | 5.91:1 | 9.40:1 |
| Primary text / surface | 14.80:1 | 14.08:1 |
| Secondary text / surface | 6.17:1 | 8.35:1 |
| Accent / paper, including solid-button labels | 6.74:1 | 10.14:1 |
| Accent / selected tint | 6.27:1 | 6.99:1 |
| Error / notice tint | 6.44:1 | 7.10:1 |
| Input border / paper | 3.51:1 | 5.31:1 |
| Input border / surface | 3.67:1 | 4.71:1 |

All listed enabled text pairs exceed 4.5:1. Input boundaries use `controlBorder` (`#7B897F` in light mode; `#829188` in dark mode), exceeding 3:1 against their adjacent backgrounds. Decorative separators use a quieter `line` token; it is not suitable by itself for a field boundary that needs 3:1 contrast.

## Verification scope

Check the catalogue, category and alphabet pickers, saved list, account forms, reader and administration at 320-pixel width and with enlarged system text. Include long Malayalam titles, mixed scripts, missing-script songs, empty/loading/error states, keyboard appearance and both color schemes.

Code inspection and contrast calculations do not establish native screen-reader or touch behavior. Before release, verify VoiceOver/TalkBack labels and focus, native switch touch areas, keyboard scrolling, text scaling, and iOS/Android rendering on devices. Browser preview checks supplement those native checks.
