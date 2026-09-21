import { Platform } from "react-native";
import * as AppleAuthentication from "expo-apple-authentication";
import { api } from "./api";
export const googleConfigured =
  !!process.env.EXPO_PUBLIC_GOOGLE_WEB_CLIENT_ID &&
  (Platform.OS !== "ios" || !!process.env.EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID);
export async function googleCredential() {
  const { nonce } = await api<{ nonce: string }>("/auth/challenge");
  const { GoogleOneTapSignIn, isSuccessResponse } =
    await import("react-native-nitro-google-signin");
  GoogleOneTapSignIn.configure({
    webClientId: process.env.EXPO_PUBLIC_GOOGLE_WEB_CLIENT_ID!,
    iosClientId: process.env.EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID,
    nonce,
  });
  await GoogleOneTapSignIn.checkPlayServices();
  const result = await GoogleOneTapSignIn.presentExplicitSignIn();
  if (!isSuccessResponse(result)) return null;
  return { idToken: result.data.idToken, nonce };
}
export async function appleCredential() {
  const { nonce } = await api<{ nonce: string }>("/auth/challenge");
  const result = await AppleAuthentication.signInAsync({
    nonce,
    requestedScopes: [
      AppleAuthentication.AppleAuthenticationScope.FULL_NAME,
      AppleAuthentication.AppleAuthenticationScope.EMAIL,
    ],
  });
  if (!result.authorizationCode)
    throw new Error("Apple did not return an authorization code.");
  if (!result.identityToken)
    throw new Error("Apple did not return a sign-in credential.");
  return {
    idToken: result.identityToken,
    nonce,
    authorizationCode: result.authorizationCode,
  };
}
