# Holy Hymns artwork and fonts

`icon.svg` is original Holy Hymns artwork: a cross above an open hymnbook in forest green (`#214733`) on warm paper (`#F8F5ED`). `icon.png` is the opaque 1024px app icon; `splash.png` uses the same mark. Regenerate the PNG/vector pair with `node assets/generate-icon.cjs` after `npm ci`. The renderer uses Expo's existing Jimp dependency; no external image service is needed.

The app uses the platform system font for interface and Manglish text, and bundles Noto Sans Malayalam Regular through `@expo-google-fonts/noto-sans-malayalam`. The bundled font is distributed under the SIL Open Font License 1.1. Its complete copyright notice and license terms are copied in `licenses/`. Font data stays local to the app. Ionicons is supplied by `@expo/vector-icons` under MIT; its upstream license remains in the locked dependency distribution.
