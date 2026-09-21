# Holy Hymns mobile app

React Native and Expo SDK 57; one codebase for iOS and Android. The optional web preview uses the same reader and admin UI. No hosted Expo services or paid Google sign-in packages are required.

## Local development

Use Node 22.13 or newer. Copy `.env.example` to `.env`, configure the API, then:

```sh
npm ci
npm run web
npm run ios
npm run android
```

Native commands create a local development build. Google authentication needs this build and will not work in Expo Go. Xcode (including an iOS simulator) or an Android SDK/emulator is required. Use your computer's LAN address for a physical device; Android emulator `localhost` refers to the emulator, so use `http://10.0.2.2:8080/v1`. Use HTTPS for deployed builds. Local HTTP development uses the development build's platform transport configuration; never enable unrestricted cleartext transport in a release.

Native identifiers default to `org.holyhymns.app`. Set `IOS_BUNDLE_ID` and `ANDROID_PACKAGE` before registering OAuth clients or signing releases. No EAS subscription is needed: archive the generated iOS project in Xcode and build/sign an Android app bundle with Gradle or Android Studio.

## Install a Release build on your iPhone

Sign in to your Apple Account in Xcode, connect and trust the iPhone, and enable Developer Mode on it. From `mobile/`, set the signing team shown by Xcode and the live API before generating and building the app:

```sh
export EXPO_PUBLIC_API_URL=https://holyhymns-backend.grejo.in/v1
export IOS_APPLE_TEAM_ID=YOUR_TEAM_ID
# Required for a free Personal Team; omit for a team with Apple Sign in enabled.
export IOS_APPLE_SIGN_IN_ENABLED=false
npx expo prebuild --platform ios --no-install
npx expo run:ios --device --configuration Release
```

Replace `YOUR_TEAM_ID` with your team identifier. Expo SDK 57 prebuild regenerates the ignored native iOS project by default, removing prior manual native edits and installed Pods. To apply configuration to an existing native project while preserving its Pods, add `--no-clean`. `IOS_APPLE_TEAM_ID` configures signing; private signing credentials stay in Xcode. `IOS_APPLE_SIGN_IN_ENABLED` defaults to `true`; only the exact value `false` removes Apple Sign in from the generated entitlements and hides its login button. A local config plugin also handles Expo's automatically applied Apple plugin; the runtime library remains installed. Browsing and email accounts remain supported in a Personal Team build.

The Release app embeds its JavaScript and fonts, so it runs without Metro or the Mac after installation; catalogue access still needs internet. Free Personal Team provisioning expires after seven days, requiring a rebuild and reinstall. [Apple's Personal Team limits](https://developer.apple.com/help/account/basics/about-your-developer-account), [Expo local builds](https://docs.expo.dev/guides/local-app-development/).

For Xcode 27 / iOS 27, the existing `expo-build-properties` configuration enables `ios.enableSceneSupport`. Expo SDK 57 requires this opt-in so the generated app uses its scene delegate; otherwise iOS 27 stops it at launch. Keep Expo at 57.0.23 or newer and regenerate native configuration after changing this setting. The native configuration test generates an isolated project from the installed template to verify scene startup, links, splash configuration, and Personal Team entitlements. [Expo's SDK 57 scene lifecycle guidance](https://github.com/expo/fyi/blob/main/ios-scene-lifecycle.md#staying-on-sdk-57-with-xcode-27).

## Provider configuration

Email/password authentication works against the self-hosted API. The app supports verification, password reset and admin invitation deep links: `holyhymns://auth/verify?token=…`, `holyhymns://auth/reset-password?token=…`, `holyhymns://auth/accept-invitation?token=…`. Manual token-entry screens are available too. An invitation for an existing account must be accepted while signed in to that account.

Google uses MIT-licensed `react-native-nitro-google-signin`, with Android Credential Manager and Google's iOS SDK. Configure Google Cloud Web, iOS and Android clients; add the release signing fingerprint to the Android client. Set `EXPO_PUBLIC_GOOGLE_WEB_CLIENT_ID`, `EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID`, and `GOOGLE_IOS_URL_SCHEME` (the reversed iOS client ID, `com.googleusercontent.apps.…`). There are no client secrets in the app. List the required token audiences in the backend's Google client IDs. A fresh server nonce is passed unchanged to the native SDK and backend for every sign-in or link operation. Native Google buttons only appear when both the backend and relevant mobile configuration are enabled.

Apple uses `expo-apple-authentication`. Enable Sign in with Apple for the final bundle identifier and signing profile; configure that identifier as an Apple audience on the server. The system-provided button appears only on supported Apple devices when the backend enables the provider. Apple and Google flows must be tested with production credentials on actual devices before submission. The web preview intentionally exposes email accounts only.

Account linking requires an already authenticated Holy Hymns session. Equal email addresses never merge accounts in the app. Native session tokens are kept in SecureStore; web preview tokens live only in sessionStorage and are removed on sign-out.

## Content and privacy

The UI contains no sample songs. It reads only published catalogue content from the API. Administration appears on the ordinary Account screen for authorized admin/owner accounts. Draft editing, preview, publication, unpublication, revision restoration, categories, app settings, access management and analytics are available in that panel.

The foreground app streams public content invalidations with `expo/fetch`, reconciles on reconnect/foreground and keeps the same reader scroll view mounted while lyrics change. Account permissions are always enforced by the server. Favorites are local for guests, merged into the account on first sign-in, and synchronized with persisted additions/removals. Account favorite storage is separated by account ID. Withdrawn songs are omitted from the saved list and do not block its sync queue.

Analytics are opt-in and disabled initially. Events contain only event type, song/category IDs or result counts; the client does not send raw search text or email addresses. The backend also excludes administrator events. Font files are bundled locally; there are no remote font requests.

## Verification

```sh
npm run typecheck
npm test
npx expo install --check
npm run export:web
npm run export:native
```

Before release: test VoiceOver/TalkBack, Malayalam shaping on a real iPhone and Android, deep links from actual email, provider login/linking/deletion, offline failures, publication across two devices, reconnect and background resume. Test the production API origin with HTTPS and the final app signing credentials. The original hymnbook/cross icon and splash mark are bundled in `assets/`, along with font license notices. Supply real screenshots, a hosted privacy policy, a review account and store metadata before submitting. App-store memberships are separate platform costs.

Official references: [Expo SDK matrix](https://docs.expo.dev/versions/latest/), [streaming fetch](https://docs.expo.dev/versions/latest/sdk/expo/#expofetch-api), [Google authentication](https://docs.expo.dev/guides/google-authentication/), [Nitro Google Sign-In source and MIT license](https://github.com/react-native-nitro-google-sign-in/google-signin), [Apple authentication](https://docs.expo.dev/versions/latest/sdk/apple-authentication/).
