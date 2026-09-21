const { withEntitlementsPlist, withPlugins } = require("expo/config-plugins");

/** @type {import('expo/config-plugins').ConfigPlugin} */
module.exports = function withOptionalAppleSignIn(config) {
  if (config.ios?.usesAppleSignIn === false) {
    // Mods run in reverse registration order: remove after Apple's mod writes.
    config = withEntitlementsPlist(config, (mod) => {
      delete mod.modResults["com.apple.developer.applesignin"];
      return mod;
    });
  }

  // Invoke the official run-once plugin so Expo's automatic SDK plugin pass
  // cannot apply it again after our optional entitlement handling.
  return withPlugins(config, ["expo-apple-authentication"]);
};
