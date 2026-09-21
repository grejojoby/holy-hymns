// Web preview supports self-hosted email accounts. Native builds use social.native.ts.
export const googleConfigured = false;
export async function googleCredential(): Promise<{
  idToken: string;
  nonce: string;
} | null> {
  throw new Error("Google sign-in is available in the iOS and Android apps.");
}
export async function appleCredential(): Promise<{
  idToken: string;
  nonce: string;
} | null> {
  throw new Error("Apple sign-in is available in the iOS app.");
}
