import type { ExpoConfig } from "expo/config";

const appleSignInEnabled = process.env.IOS_APPLE_SIGN_IN_ENABLED !== "false";
const iosGoogleID = process.env.EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID;
const googleScheme =
  process.env.GOOGLE_IOS_URL_SCHEME ||
  (iosGoogleID?.endsWith(".apps.googleusercontent.com")
    ? `com.googleusercontent.apps.${iosGoogleID.slice(0, -".apps.googleusercontent.com".length)}`
    : undefined);
const config: ExpoConfig = {
  name: "Holy Hymns",
  slug: "holy-hymns",
  version: "1.0.0",
  scheme: "holyhymns",
  icon: "./assets/icon.png",
  orientation: "default",
  userInterfaceStyle: "automatic",
  ios: {
    bundleIdentifier: process.env.IOS_BUNDLE_ID || "org.holyhymns.app",
    appleTeamId: process.env.IOS_APPLE_TEAM_ID || undefined,
    supportsTablet: true,
    usesAppleSignIn: appleSignInEnabled,
    infoPlist: { ITSAppUsesNonExemptEncryption: false },
  },
  android: {
    package: process.env.ANDROID_PACKAGE || "org.holyhymns.app",
    adaptiveIcon: {
      foregroundImage: "./assets/icon.png",
      backgroundColor: "#F8F5ED",
    },
  },
  plugins: [
    "expo-secure-store",
    [
      "expo-splash-screen",
      {
        image: "./assets/splash.png",
        imageWidth: 240,
        resizeMode: "contain",
        backgroundColor: "#F8F5ED",
      },
    ],
    [
      "expo-build-properties",
      {
        ios: {
          enableSceneSupport: true,
          extraPods: [
            { name: "GoogleUtilities", modular_headers: true },
            { name: "RecaptchaInterop", modular_headers: true },
          ],
        },
      },
    ],
    "./plugins/with-optional-apple-sign-in",
    ...(googleScheme
      ? [
          [
            "react-native-nitro-google-signin",
            { iosUrlScheme: googleScheme },
          ] as [string, object],
        ]
      : []),
  ],
  web: {
    name: "Holy Hymns",
    shortName: "Holy Hymns",
    bundler: "metro",
    favicon: "./assets/icon.png",
  },
  experiments: { tsconfigPaths: true },
};
export default config;
