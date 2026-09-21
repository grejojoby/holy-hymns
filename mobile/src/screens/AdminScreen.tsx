import React, { useEffect, useState } from "react";
import { BackHandler, ScrollView, View } from "react-native";
import { useApp } from "../hooks/AppContext";
import { useResource } from "../hooks/useResource";
import { query } from "../lib/api";
import type { List, Song } from "../lib/types";
import {
  Button,
  Copy,
  Empty,
  Field,
  Heading,
  Loading,
  Page,
  Pill,
  useTheme,
} from "../components/ui";
import {
  AnalyticsScreen,
  CategoryManager,
  ConfigManager,
  UserManager,
} from "./AdminSettings";
import { SongEditor } from "./SongEditor";
export function AdminScreen({ close }: { close: () => void }) {
  const theme = useTheme();
  const { user } = useApp();
  const [section, setSection] = useState("songs");
  const [editor, setEditor] = useState<string | null | undefined>(undefined);
  const [version, setVersion] = useState(0);
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");
  const [offset, setOffset] = useState(0);
  useEffect(() => {
    // The editor owns Back while open so its unsaved-change guard runs first.
    if (editor !== undefined && user && user.role !== "reader") return;
    const subscription = BackHandler.addEventListener(
      "hardwareBackPress",
      () => {
        close();
        return true;
      },
    );
    return () => subscription.remove();
  }, [close, editor, user?.role]);
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebounced(search);
      setOffset(0);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);
  const songs = useResource<List<Song>>(
    user &&
      user.role !== "reader" &&
      section === "songs" &&
      editor === undefined
      ? `/admin/songs?${query({ q: debounced, offset, limit: 30 })}`
      : null,
    version,
  );
  if (!user || user.role === "reader")
    return (
      <Page>
        <Empty
          title="Administration unavailable"
          detail="Sign in with an authorized account to manage the collection."
        />
        <Button title="Back to account" onPress={close} />
      </Page>
    );
  if (editor !== undefined)
    return (
      <SongEditor
        id={editor}
        close={() => {
          setEditor(undefined);
          setVersion((value) => value + 1);
        }}
      />
    );
  return (
    <Page>
      <View style={{ gap: 12, alignItems: "flex-start" }}>
        <Button
          title="Account"
          icon="arrow-back"
          variant="quiet"
          onPress={close}
        />
        <Heading level={2}>Manage Holy Hymns</Heading>
      </View>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={{ gap: 8 }}
      >
        {["songs", "categories", "settings", "people", "analytics"].map(
          (value) => (
            <Pill
              key={value}
              label={value[0]!.toUpperCase() + value.slice(1)}
              active={section === value}
              onPress={() => setSection(value)}
            />
          ),
        )}
      </ScrollView>
      {section === "songs" ? (
        <View style={{ gap: 20 }}>
          <Button
            title="Add hymn"
            icon="add-outline"
            onPress={() => setEditor(null)}
          />
          <Field
            label="Search songs"
            placeholder="Search titles"
            value={search}
            onChangeText={setSearch}
          />
          {songs.loading ? (
            <Loading />
          ) : songs.error ? (
            <Empty
              title="Unable to load songs"
              detail={songs.error}
              retry={songs.reload}
            />
          ) : !songs.data?.items.length ? (
            <Empty
              title={search ? "No matching songs" : "No songs yet"}
              detail={
                search
                  ? "Try another title."
                  : "Add your first hymn to get started."
              }
            />
          ) : (
            <View>
              {songs.data.items.map((song) => (
                <View
                  key={song.id}
                  style={{
                    flexDirection: "row",
                    alignItems: "center",
                    gap: 12,
                    paddingVertical: 16,
                    borderBottomWidth: 1,
                    borderColor: theme.line,
                  }}
                >
                  <View style={{ flex: 1, gap: 4 }}>
                    <Copy style={{ fontWeight: "600" }}>
                      {song.title || "Untitled draft"}
                    </Copy>
                    <Copy muted style={{ fontSize: 14 }}>
                      {song.status === "published" ? "Published" : "Draft"}
                      {song.reviewNotes?.length ? " · Needs review" : ""}
                      {song.featured ? " · Featured" : ""}
                    </Copy>
                  </View>
                  <Button
                    title="Edit"
                    variant="quiet"
                    small
                    onPress={() => setEditor(song.id)}
                  />
                </View>
              ))}
            </View>
          )}
          <View
            style={{ flexDirection: "row", justifyContent: "space-between" }}
          >
            <Button
              title="Previous"
              disabled={!offset || songs.loading}
              variant="quiet"
              onPress={() => setOffset(Math.max(0, offset - 30))}
            />
            <Button
              title="Next"
              disabled={(songs.data?.items.length || 0) < 30 || songs.loading}
              variant="quiet"
              onPress={() => setOffset(offset + 30)}
            />
          </View>
        </View>
      ) : section === "categories" ? (
        <CategoryManager />
      ) : section === "settings" ? (
        <ConfigManager />
      ) : section === "people" ? (
        <UserManager />
      ) : (
        <AnalyticsScreen />
      )}
    </Page>
  );
}
