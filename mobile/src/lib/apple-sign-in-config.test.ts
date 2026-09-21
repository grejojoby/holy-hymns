import assert from "node:assert/strict";
import { mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

const projectRoot = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(import.meta.url);

test("Expo's automatic Apple plugin respects the optional signing capability", () => {
  const config = require(join(projectRoot, "app.config.ts")).default;
  assert.ok(config.plugins.includes("./plugins/with-optional-apple-sign-in"));
  const fixture = mkdtempSync(join(tmpdir(), "holy-hymns-apple-plugin-"));
  try {
    symlinkSync(
      join(projectRoot, "node_modules"),
      join(fixture, "node_modules"),
      "dir",
    );
    writeFileSync(
      join(fixture, "package.json"),
      JSON.stringify({
        name: "holy-hymns-plugin-check",
        version: "1.0.0",
        dependencies: {
          expo: require("expo/package.json").version,
          "expo-apple-authentication":
            require("expo-apple-authentication/package.json").version,
        },
      }),
    );
    for (const enabled of [undefined, true, false]) {
      writeFileSync(
        join(fixture, "app.json"),
        JSON.stringify({
          expo: {
            name: "Plugin Check",
            slug: "plugin-check",
            ios: {
              bundleIdentifier: "org.holyhymns.plugincheck",
              usesAppleSignIn: enabled,
              entitlements: {
                "com.apple.developer.applesignin": ["Default"],
                "com.apple.developer.healthkit": true,
              },
            },
            plugins: [
              join(projectRoot, "plugins/with-optional-apple-sign-in.js"),
            ],
          },
        }),
      );
      // Exercise Expo's complete prebuild plugin pipeline without writing iOS files.
      const result = spawnSync(
        process.execPath,
        [
          join(projectRoot, "node_modules/expo/bin/cli"),
          "config",
          "--type",
          "introspect",
          "--json",
        ],
        {
          cwd: fixture,
          encoding: "utf8",
          env: { ...process.env, EXPO_NO_DOTENV: "1", CI: "1" },
          timeout: 30000,
        },
      );
      assert.equal(
        result.status,
        0,
        result.stderr || result.error?.message || "Expo introspection failed",
      );
      const generated = JSON.parse(result.stdout);
      const entitlements = generated._internal.modResults.ios.entitlements;
      if (enabled === false) {
        assert.ok(
          !Object.hasOwn(entitlements, "com.apple.developer.applesignin"),
        );
      } else {
        assert.deepEqual(entitlements["com.apple.developer.applesignin"], [
          "Default",
        ]);
      }
      assert.equal(entitlements["com.apple.developer.healthkit"], true);
      assert.ok(generated._internal.pluginHistory["expo-apple-authentication"]);
    }
  } finally {
    rmSync(fixture, { recursive: true, force: true });
  }
});
