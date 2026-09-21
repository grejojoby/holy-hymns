import React, { useEffect, useState } from "react";
import {
  ActivityIndicator,
  BackHandler,
  Linking,
  Pressable,
  Text,
  View,
} from "react-native";
import { SafeAreaProvider, SafeAreaView } from "react-native-safe-area-context";
import { StatusBar } from "expo-status-bar";
import { useFonts } from "expo-font";
import { NotoSansMalayalam_400Regular } from "@expo-google-fonts/noto-sans-malayalam/400Regular";
import Ionicons from "@expo/vector-icons/Ionicons";
import { AppProvider, useApp } from "./src/hooks/AppContext";
import { useResource } from "./src/hooks/useResource";
import type { AppConfig } from "./src/lib/types";
import { useTheme } from "./src/components/ui";
import { CatalogScreen } from "./src/screens/CatalogScreen";
import { SavedScreen } from "./src/screens/SavedScreen";
import {
  AccountScreen,
  type AuthLink,
  type AuthMode,
} from "./src/screens/AccountScreen";
import { SongScreen } from "./src/screens/SongScreen";
import { AdminScreen } from "./src/screens/AdminScreen";

type Tab = "collection" | "saved" | "account";
function Shell() {
  const { ready, user, revision } = useApp();
  const theme = useTheme();
  const config = useResource<AppConfig>("/config", revision);
  const [tab, setTab] = useState<Tab>("collection");
  const [song, setSong] = useState<string | null>(null);
  const [admin, setAdmin] = useState(false);
  const [authLink, setAuthLink] = useState<AuthLink | null>(null);
  useEffect(() => {
    if (!user || user.role === "reader") setAdmin(false);
  }, [user?.role]);
  useEffect(() => {
    const handleURL = (url: string) => {
      try {
        const parsed = new URL(url);
        const action = parsed.pathname.split("/").filter(Boolean).pop();
        if (
          parsed.protocol !== "holyhymns:" ||
          parsed.hostname !== "auth" ||
          !["verify", "reset-password", "accept-invitation"].includes(
            action || "",
          )
        )
          return;
        const token = parsed.searchParams.get("token") || "";
        setSong(null);
        setAdmin(false);
        setTab("account");
        setAuthLink({ mode: action as AuthMode, token, key: Date.now() });
      } catch {
        /* Other application links are ignored. */
      }
    };
    void Linking.getInitialURL().then((url) => {
      if (url) handleURL(url);
    });
    const subscription = Linking.addEventListener("url", ({ url }) =>
      handleURL(url),
    );
    return () => subscription.remove();
  }, []);
  useEffect(() => {
    if (admin) return; // Administration registers its own guarded back actions.
    const listener = BackHandler.addEventListener("hardwareBackPress", () => {
      if (song) {
        setSong(null);
        return true;
      }
      if (tab !== "collection") {
        setTab("collection");
        return true;
      }
      return false;
    });
    return () => listener.remove();
  }, [song, admin, tab]);
  return (
    <SafeAreaView
      style={{ flex: 1, backgroundColor: theme.paper }}
      edges={["top", "bottom", "left", "right"]}
    >
      <StatusBar style={theme.dark ? "light" : "dark"} />
      {!song && !admin && (
        <View
          style={{
            width: "100%",
            maxWidth: 680,
            alignSelf: "center",
            paddingHorizontal: 16,
            paddingTop: 12,
            paddingBottom: 8,
          }}
        >
          <Text
            accessibilityRole="header"
            style={{
              fontSize: 20,
              lineHeight: 28,
              fontWeight: "600",
              letterSpacing: -0.4,
              color: theme.ink,
            }}
          >
            {tab === "collection"
              ? config.data?.appName || "Holy Hymns"
              : tab === "saved"
                ? "Saved hymns"
                : "Account"}
          </Text>
        </View>
      )}
      {!ready ? (
        <ActivityIndicator style={{ flex: 1 }} color={theme.accent} />
      ) : (
        <>
          <View
            style={{ flex: 1, display: song || admin ? "none" : "flex" }}
            accessibilityElementsHidden={!!song || admin}
            importantForAccessibility={
              song || admin ? "no-hide-descendants" : "auto"
            }
          >
            {tab === "collection" ? (
              <CatalogScreen openSong={setSong} />
            ) : tab === "saved" ? (
              <SavedScreen openSong={setSong} />
            ) : (
              <AccountScreen
                openAdmin={() => setAdmin(true)}
                authLink={authLink}
              />
            )}
          </View>
          {song ? (
            <SongScreen id={song} close={() => setSong(null)} />
          ) : admin ? (
            <AdminScreen close={() => setAdmin(false)} />
          ) : null}
        </>
      )}
      {!song && !admin && (
        <View
          style={{
            flexDirection: "row",
            borderTopWidth: 1,
            borderColor: theme.line,
            paddingTop: 5,
            paddingBottom: 3,
            backgroundColor: theme.surface,
          }}
        >
          {(
            [
              {
                key: "collection",
                label: "Hymns",
                icon: "book-outline",
                active: "book",
              },
              {
                key: "saved",
                label: "Saved",
                icon: "bookmark-outline",
                active: "bookmark",
              },
              {
                key: "account",
                label: "Account",
                icon: "person-outline",
                active: "person",
              },
            ] as const
          ).map((item) => (
            <Pressable
              key={item.key}
              accessibilityRole="tab"
              accessibilityLabel={item.label}
              accessibilityState={{ selected: tab === item.key }}
              onPress={() => setTab(item.key)}
              style={({ pressed }) => ({
                flex: 1,
                alignItems: "center",
                minHeight: 52,
                paddingVertical: 5,
                gap: 3,
                opacity: pressed ? 0.6 : 1,
              })}
            >
              <Ionicons
                name={tab === item.key ? item.active : item.icon}
                size={21}
                color={tab === item.key ? theme.accent : theme.muted}
              />
              <Text
                style={{
                  fontWeight: tab === item.key ? "600" : "400",
                  fontSize: 12,
                  lineHeight: 18,
                  color: tab === item.key ? theme.accent : theme.muted,
                }}
              >
                {item.label}
              </Text>
            </Pressable>
          ))}
        </View>
      )}
    </SafeAreaView>
  );
}
export default function App() {
  const [loaded, error] = useFonts({
    Malayalam: NotoSansMalayalam_400Regular,
  });
  if (!loaded && !error)
    return (
      <View
        style={{
          flex: 1,
          backgroundColor: "#FAFAF8",
          justifyContent: "center",
        }}
      >
        <ActivityIndicator color="#38614C" />
      </View>
    );
  return (
    <SafeAreaProvider>
      <AppProvider>
        <Shell />
      </AppProvider>
    </SafeAreaProvider>
  );
}
