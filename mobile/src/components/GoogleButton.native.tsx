import React from "react";
import { GoogleSignInButton } from "react-native-nitro-google-signin";
export function GoogleButton({
  onPress,
  busy,
}: {
  onPress: () => void;
  busy: boolean;
}) {
  return (
    <GoogleSignInButton
      signInBehavior="none"
      size="wide"
      colorScheme="light"
      disabled={busy}
      loading={busy}
      onPress={onPress}
      accessibilityLabel="Continue with Google"
      style={{ width: "100%", height: 48 }}
    />
  );
}
