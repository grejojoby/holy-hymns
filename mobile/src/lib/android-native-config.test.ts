import assert from "node:assert/strict";
import {
  copyFileSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

const projectRoot = fileURLToPath(new URL("../../", import.meta.url));

test("Android prebuild preserves links, rotation, keyboard resizing and secure storage", () => {
  const fixture = mkdtempSync(join(tmpdir(), "holy-hymns-android-config-"));
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
        "android",
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
          ANDROID_PACKAGE: "org.holyhymns.androidtest",
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
    const manifest = readFileSync(
      join(fixture, "android/app/src/main/AndroidManifest.xml"),
      "utf8",
    );
    assert.match(manifest, /android:name="android.permission.INTERNET"/);
    assert.match(manifest, /android:scheme="holyhymns"/);
    assert.match(manifest, /android:launchMode="singleTask"/);
    assert.match(manifest, /android:screenOrientation="unspecified"/);
    assert.match(manifest, /android:windowSoftInputMode="adjustResize"/);
    assert.doesNotMatch(manifest, /android:usesCleartextTraffic="true"/);
    const gradle = readFileSync(
      join(fixture, "android/app/build.gradle"),
      "utf8",
    );
    assert.match(gradle, /applicationId ['"]org\.holyhymns\.androidtest['"]/);
    assert.match(gradle, /versionCode 1/);
    // Backup rules are resources supplied by the SecureStore Android library.
    assert.match(
      manifest,
      /android:fullBackupContent="@xml\/secure_store_backup_rules"/,
    );
    assert.match(
      manifest,
      /android:dataExtractionRules="@xml\/secure_store_data_extraction_rules"/,
    );
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});
