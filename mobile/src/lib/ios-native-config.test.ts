import assert from "node:assert/strict";
import {
  copyFileSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
} from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

const projectRoot = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(import.meta.url);
const plist = require("@expo/plist").default;

test("native prebuild adopts scenes while preserving links, splash, and Personal Team signing", () => {
  const fixture = mkdtempSync(join(tmpdir(), "holy-hymns-native-config-"));
  try {
    for (const directory of ["node_modules", "assets", "plugins"]) {
      symlinkSync(
        join(projectRoot, directory),
        join(fixture, directory),
        "dir",
      );
    }
    for (const file of ["package.json", "app.config.ts"]) {
      copyFileSync(join(projectRoot, file), join(fixture, file));
    }
    // Use the installed template and skip installation: no network, Xcode, Pods,
    // signing credentials, or changes to the app's working native project.
    const result = spawnSync(
      process.execPath,
      [
        join(projectRoot, "node_modules/expo/bin/cli"),
        "prebuild",
        "--platform",
        "ios",
        "--no-install",
        "--no-clean",
        "--template",
        join(projectRoot, "node_modules/expo/template.tgz"),
      ],
      {
        cwd: fixture,
        encoding: "utf8",
        timeout: 60000,
        env: {
          ...process.env,
          CI: "1",
          EXPO_OFFLINE: "1",
          EXPO_NO_DOTENV: "1",
          IOS_APPLE_SIGN_IN_ENABLED: "false",
          IOS_APPLE_TEAM_ID: "",
          IOS_BUNDLE_ID: "org.holyhymns.scenetest",
          EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID: "",
          EXPO_PUBLIC_GOOGLE_WEB_CLIENT_ID: "",
          GOOGLE_IOS_URL_SCHEME: "",
        },
      },
    );
    assert.equal(
      result.status,
      0,
      result.stderr || result.error?.message || "Native prebuild failed",
    );
    const appDirectory = join(fixture, "ios", "HolyHymns");
    const info = plist.parse(
      readFileSync(join(appDirectory, "Info.plist"), "utf8"),
    );
    const sceneManifest = info.UIApplicationSceneManifest;
    assert.equal(sceneManifest.UIApplicationSupportsMultipleScenes, false);
    assert.equal(
      sceneManifest.UISceneConfigurations.UIWindowSceneSessionRoleApplication[0]
        .UISceneDelegateClassName,
      "EXExpoAppSceneDelegate",
    );
    const appDelegate = readFileSync(
      join(appDirectory, "AppDelegate.swift"),
      "utf8",
    );
    assert.match(
      appDelegate,
      /class AppDelegate: ExpoAppDelegate, ExpoReactNativeFactoryProvider/,
    );
    assert.match(appDelegate, /reactNativeFactory = factory/);
    assert.doesNotMatch(appDelegate, /factory\.startReactNative\(/);
    assert.doesNotMatch(
      appDelegate,
      /UIWindow\(frame: UIScreen\.main\.bounds\)/,
    );
    assert.ok(
      info.CFBundleURLTypes.some((type: { CFBundleURLSchemes: string[] }) =>
        type.CFBundleURLSchemes.includes("holyhymns"),
      ),
    );
    assert.equal(info.UILaunchStoryboardName, "SplashScreen");
    const entitlements = plist.parse(
      readFileSync(join(appDirectory, "HolyHymns.entitlements"), "utf8"),
    );
    assert.ok(!Object.hasOwn(entitlements, "com.apple.developer.applesignin"));
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});
