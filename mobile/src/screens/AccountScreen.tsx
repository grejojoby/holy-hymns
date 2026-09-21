import React, { useEffect, useState } from "react";
import { Linking, Platform, View } from "react-native";
import * as AppleAuthentication from "expo-apple-authentication";
import { GoogleButton } from "../components/GoogleButton";
import { useApp } from "../hooks/AppContext";
import { useResource } from "../hooks/useResource";
import { api, message, post } from "../lib/api";
import {
  appleCredential,
  googleConfigured,
  googleCredential,
} from "../lib/social";
import { writeLocal } from "../lib/storage";
import type { AppConfig, AuthResult } from "../lib/types";
import {
  Button,
  Copy,
  Field,
  Heading,
  Notice,
  Page,
  Panel,
  Pill,
  Toggle,
  useTheme,
} from "../components/ui";

export type AuthMode =
  | "login"
  | "register"
  | "forgot-password"
  | "verify"
  | "reset-password"
  | "accept-invitation";
export type AuthLink = { mode: AuthMode; token: string; key: number };
export function AccountScreen({
  openAdmin,
  authLink,
}: {
  openAdmin: () => void;
  authLink: AuthLink | null;
}) {
  const theme = useTheme();
  const {
    user,
    acceptSession,
    logout,
    clearSession,
    preferences,
    setPreferences,
    revision,
  } = useApp();
  const [mode, setMode] = useState<AuthMode>("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [appleAvailable, setAppleAvailable] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState("");
  const [privacy, setPrivacy] = useState(false);
  const [manualRevocationUrl, setManualRevocationUrl] = useState("");
  const [emailTools, setEmailTools] = useState(false);
  const providers = useResource<{ google: boolean; apple: boolean }>(
    "/auth/providers",
  );
  const config = useResource<AppConfig>("/config", revision);
  useEffect(() => {
    void AppleAuthentication.isAvailableAsync().then(setAppleAvailable);
  }, []);
  useEffect(() => {
    if (authLink) {
      setMode(authLink.mode);
      setToken(authLink.token);
      setError("");
      setNotice("");
    }
  }, [authLink?.key]);
  const run = async (task: () => Promise<void>) => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await task();
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  };
  const switchMode = (next: AuthMode) => {
    setMode(next);
    setError("");
    setNotice("");
    setPassword("");
  };
  const submit = () =>
    run(async () => {
      if (mode === "login") {
        await acceptSession(
          await post<AuthResult>("/auth/login", { email, password }),
        );
        setPassword("");
        return;
      }
      const body =
        mode === "register"
          ? { email, password, name }
          : mode === "forgot-password"
            ? { email }
            : mode === "verify"
              ? { token }
              : mode === "accept-invitation"
                ? { token, password, name }
                : { token, password };
      const result = await post<{ message?: string }>(`/auth/${mode}`, body);
      setNotice(
        result?.message ||
          (mode === "verify"
            ? "Email verified. You can now sign in."
            : mode === "reset-password"
              ? "Password changed. You can now sign in."
              : mode === "accept-invitation"
                ? "Invitation accepted. Sign in to manage Holy Hymns."
                : "Check your email for the next step."),
      );
      setPassword("");
      if (
        mode === "verify" ||
        mode === "reset-password" ||
        mode === "accept-invitation"
      ) {
        setMode("login");
        setToken("");
      }
    });
  const social = (provider: "google" | "apple") =>
    run(async () => {
      try {
        const credential = await (provider === "google"
          ? googleCredential()
          : appleCredential());
        if (!credential) return;
        if (user) {
          await post(`/auth/link/${provider}`, credential);
          setNotice(
            `${provider === "google" ? "Google" : "Apple"} sign-in linked to your account.`,
          );
        } else
          await acceptSession(
            await post<AuthResult>(`/auth/social/${provider}`, credential),
          );
      } catch (error) {
        if ((error as { code?: string })?.code === "ERR_REQUEST_CANCELED")
          return;
        throw error;
      }
    });
  const hasProviders =
    (providers.data?.google && googleConfigured) ||
    (providers.data?.apple && appleAvailable);
  const providerButtons = hasProviders ? (
    <View style={{ gap: 12 }}>
      {providers.data?.google && googleConfigured && (
        <View style={{ gap: 6 }}>
          {user && <Copy muted>Link Google sign-in</Copy>}
          <GoogleButton
            busy={busy}
            onPress={() => {
              void social("google");
            }}
          />
        </View>
      )}
      {providers.data?.apple && appleAvailable && (
        <View style={{ gap: 6 }}>
          {user && <Copy muted>Link Apple sign-in</Copy>}
          <AppleAuthentication.AppleAuthenticationButton
            buttonType={
              AppleAuthentication.AppleAuthenticationButtonType.CONTINUE
            }
            buttonStyle={
              theme.dark
                ? AppleAuthentication.AppleAuthenticationButtonStyle.WHITE
                : AppleAuthentication.AppleAuthenticationButtonStyle.BLACK
            }
            cornerRadius={12}
            style={{ height: 48, width: "100%", opacity: busy ? 0.5 : 1 }}
            onPress={() => {
              if (!busy) void social("apple");
            }}
          />
        </View>
      )}
    </View>
  ) : null;
  return (
    <Page>
      {user && (
        <View style={{ gap: 4 }}>
          {!!user.name && (
            <Copy style={{ fontSize: 18, fontWeight: "600" }}>{user.name}</Copy>
          )}
          <Copy muted>{user.email}</Copy>
        </View>
      )}
      <Notice text={error} error />
      <Notice text={notice} />
      {!!manualRevocationUrl && /^https:\/\//.test(manualRevocationUrl) && (
        <Button
          title="Open Apple account settings"
          variant="outline"
          onPress={() => {
            void Linking.openURL(manualRevocationUrl);
          }}
        />
      )}
      {user ? (
        <>
          {(user.role === "admin" || user.role === "owner") && (
            <Button
              title="Manage Holy Hymns"
              icon="create-outline"
              variant="outline"
              onPress={openAdmin}
            />
          )}
          <Panel>
            <Heading level={3}>Sign-in & security</Heading>
            {providerButtons}
            <Button
              title="Sign out"
              variant="outline"
              busy={busy}
              onPress={() => {
                void run(() => logout());
              }}
            />
            <Button
              title="Sign out on all devices"
              variant="quiet"
              busy={busy}
              onPress={() => {
                void run(() => logout(true));
              }}
            />
            <Button
              title={deleting ? "Keep my account" : "Delete account"}
              variant="danger"
              onPress={() => setDeleting((value) => !value)}
            />
            {deleting && (
              <View style={{ gap: 16 }}>
                <Copy>
                  Deleting your account permanently removes your account and
                  synchronized favourites. If you use social sign-in, sign out
                  and sign back in first.
                </Copy>
                <Field
                  label="Current password (email accounts)"
                  value={password}
                  onChangeText={setPassword}
                  secureTextEntry
                  autoCapitalize="none"
                />
                <Field
                  label="Type DELETE to confirm"
                  value={deleteConfirm}
                  onChangeText={setDeleteConfirm}
                  autoCapitalize="characters"
                />
                <Button
                  title="Permanently delete account"
                  variant="danger"
                  busy={busy}
                  disabled={deleteConfirm !== "DELETE"}
                  onPress={() => {
                    void run(async () => {
                      const result = await api<{
                        manualRevocationUrl?: string;
                      }>("/auth/account", {
                        method: "DELETE",
                        body: { password: password || undefined },
                      });
                      await writeLocal(`favorites:${user.id}`, {
                        ids: [],
                        pending: {},
                      });
                      await clearSession();
                      setDeleting(false);
                      setPassword("");
                      setManualRevocationUrl(result?.manualRevocationUrl || "");
                      setNotice("Your account has been deleted.");
                    });
                  }}
                />
              </View>
            )}
          </Panel>
        </>
      ) : (
        <Panel>
          <Heading level={2}>
            {
              {
                login: "Sign in",
                register: "Create account",
                "forgot-password": "Reset password",
                verify: "Verify your email",
                "reset-password": "Set a new password",
                "accept-invitation": "Accept invitation",
              }[mode]
            }
          </Heading>
          {mode === "login" && (
            <Copy muted>Optional. Sync your saved hymns.</Copy>
          )}
          {mode === "login" && providerButtons}
          {(mode === "register" || mode === "accept-invitation") && (
            <Field
              label="Name"
              value={name}
              onChangeText={setName}
              autoComplete="name"
            />
          )}
          {(mode === "login" ||
            mode === "register" ||
            mode === "forgot-password") && (
            <Field
              label="Email address"
              value={email}
              onChangeText={setEmail}
              keyboardType="email-address"
              autoComplete="email"
              autoCapitalize="none"
              autoCorrect={false}
            />
          )}
          {(mode === "verify" ||
            mode === "reset-password" ||
            mode === "accept-invitation") && (
            <Field
              label="Code from your email"
              value={token}
              onChangeText={setToken}
              autoCapitalize="none"
              autoCorrect={false}
            />
          )}
          {mode !== "forgot-password" && mode !== "verify" && (
            <Field
              label={mode === "reset-password" ? "New password" : "Password"}
              value={password}
              onChangeText={setPassword}
              secureTextEntry
              autoCapitalize="none"
              autoComplete={
                mode === "login" ? "current-password" : "new-password"
              }
            />
          )}
          {(mode === "register" ||
            mode === "reset-password" ||
            mode === "accept-invitation") && (
            <Copy muted style={{ fontSize: 14 }}>
              Use at least 12 characters for your password.
            </Copy>
          )}
          <Button
            title={
              {
                login: "Sign in",
                register: "Create account",
                "forgot-password": "Send reset email",
                verify: "Verify email",
                "reset-password": "Update password",
                "accept-invitation": "Accept invitation",
              }[mode]
            }
            busy={busy}
            onPress={() => {
              void submit();
            }}
          />
          {mode === "login" ? (
            <>
              <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
                <Button
                  title="Create account"
                  variant="quiet"
                  onPress={() => switchMode("register")}
                />
                <Button
                  title="Forgot password?"
                  variant="quiet"
                  onPress={() => switchMode("forgot-password")}
                />
              </View>
              <Button
                title={
                  emailTools
                    ? "Hide email options"
                    : "Have a code from an email?"
                }
                variant="quiet"
                onPress={() => setEmailTools((value) => !value)}
              />
              {emailTools && (
                <View
                  style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}
                >
                  <Button
                    title="Verify email"
                    variant="outline"
                    onPress={() => switchMode("verify")}
                  />
                  <Button
                    title="Reset password"
                    variant="outline"
                    onPress={() => switchMode("reset-password")}
                  />
                </View>
              )}
            </>
          ) : (
            <Button
              title="Back to sign in"
              variant="quiet"
              onPress={() => switchMode("login")}
            />
          )}
        </Panel>
      )}
      {user &&
        !!token &&
        (mode === "accept-invitation" ||
          authLink?.mode === "accept-invitation") && (
          <Panel>
            <Heading level={3}>Admin invitation</Heading>
            <Field
              label="Invitation token"
              value={token}
              onChangeText={setToken}
              autoCapitalize="none"
            />
            <Button
              title="Accept invitation"
              busy={busy}
              onPress={() => {
                void run(async () => {
                  await post("/auth/accept-invitation", { token });
                  await clearSession();
                  setToken("");
                  setMode("login");
                  setNotice(
                    "Invitation accepted. Sign in again to manage Holy Hymns.",
                  );
                });
              }}
            />
          </Panel>
        )}
      <Panel>
        <Heading level={2}>Reading preferences</Heading>
        <Copy muted>Appearance</Copy>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
          {(["system", "light", "dark"] as const).map((value) => (
            <Pill
              key={value}
              label={value[0]!.toUpperCase() + value.slice(1)}
              active={preferences.darkMode === value}
              onPress={() => setPreferences({ darkMode: value })}
            />
          ))}
        </View>
        <Copy muted>Lyrics language</Copy>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 8 }}>
          <Pill
            label="മലയാളം"
            active={preferences.script === "malayalam"}
            onPress={() => setPreferences({ script: "malayalam" })}
          />
          <Pill
            label="Manglish"
            active={preferences.script === "manglish"}
            onPress={() => setPreferences({ script: "manglish" })}
          />
        </View>
        <Toggle
          title="Keep screen awake"
          description="While a hymn is open."
          value={preferences.keepAwake}
          onChange={(keepAwake) => setPreferences({ keepAwake })}
        />
        <Toggle
          title="Share anonymous usage"
          description="Share song opens and search result counts. No search text or email."
          value={preferences.analytics}
          onChange={(analytics) => setPreferences({ analytics })}
        />
      </Panel>
      {!!config.data?.aboutText && (
        <View style={{ gap: 12 }}>
          <Heading level={3}>About Holy Hymns</Heading>
          <Copy muted>{config.data.aboutText}</Copy>
        </View>
      )}
      {!!config.data?.supportEmail && (
        <Button
          title="Contact support"
          icon="mail-outline"
          variant="outline"
          onPress={() => {
            void Linking.openURL(`mailto:${config.data!.supportEmail}`).catch(
              (error) => setError(message(error)),
            );
          }}
        />
      )}
      <Button
        title={privacy ? "Hide privacy information" : "Privacy"}
        variant="quiet"
        onPress={() => setPrivacy((value) => !value)}
      />
      {privacy && (
        <Panel>
          <Heading level={3}>Your privacy</Heading>
          <Copy>
            You can read without an account. Saved hymns and reading preferences
            stay on this device unless you sign in. Accounts store your name,
            email, sign-in provider identifiers, and synchronized favourites on
            the Holy Hymns server.
          </Copy>
          <Copy>
            Anonymous usage sharing is optional and off by default. It records
            hymn/category IDs and search result counts, never the words you
            search for. Administrators’ activity is excluded.
          </Copy>
          <Copy>
            We do not sell personal data or use advertising trackers. Google or
            Apple processes sign-in when you choose that provider. Transactional
            email is sent through the configured mail provider. Server security
            logs can include connection metadata.
          </Copy>
          <Copy>
            You can delete your account here. Encrypted backups expire according
            to the operator’s published retention policy; retained security
            records are minimized. Contact the support address above for privacy
            or content requests.
          </Copy>
        </Panel>
      )}
      <Copy muted style={{ fontSize: 14 }}>
        Holy Hymns · v1.0.0{Platform.OS === "web" ? " · Web preview" : ""}
      </Copy>
    </Page>
  );
}
