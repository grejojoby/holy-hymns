import React, { useEffect, useRef, useState } from "react";
import { Linking, Pressable, ScrollView, Text, View } from "react-native";
import { activateKeepAwakeAsync, deactivateKeepAwake } from "expo-keep-awake";
import { useApp } from "../hooks/AppContext";
import { useResource } from "../hooks/useResource";
import { availableScript } from "../lib/logic";
import type { Song } from "../lib/types";
import {
  Button,
  Copy,
  Empty,
  Loading,
  Notice,
  Pill,
  useTheme,
} from "../components/ui";

export function SongContent({ song }: { song: Song }) {
  const { preferences, setPreferences } = useApp();
  const theme = useTheme();
  const [script, setScript] = useState(preferences.script);
  const effective = availableScript(song, script);
  const [linkError, setLinkError] = useState("");
  const title =
    effective === "malayalam" ? song.titleMalayalam || song.title : song.title;
  const alternateTitle =
    effective === "malayalam" ? song.title : song.titleMalayalam;
  const lyrics =
    effective === "malayalam" ? song.lyricsMalayalam : song.lyricsManglish;
  return (
    <View style={{ gap: 18 }}>
      <View style={{ gap: 8 }}>
        {song.status === "draft" && (
          <Notice text="Draft preview · not published" />
        )}
        <Text
          accessibilityRole="header"
          style={{
            color: theme.ink,
            fontSize: 22,
            lineHeight: 33,
            fontWeight: /[\u0D00-\u0D7F]/.test(title) ? undefined : "600",
            fontFamily: /[\u0D00-\u0D7F]/.test(title) ? "Malayalam" : undefined,
          }}
        >
          {title}
        </Text>
        {!!alternateTitle && alternateTitle !== title && (
          <Text
            style={{
              color: theme.muted,
              fontSize: 14,
              lineHeight: 24,
              fontFamily: /[\u0D00-\u0D7F]/.test(alternateTitle)
                ? "Malayalam"
                : undefined,
            }}
          >
            {alternateTitle}
          </Text>
        )}
      </View>
      <View
        style={{
          flexDirection: "row",
          flexWrap: "wrap",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 12,
        }}
      >
        {song.lyricsMalayalam && song.lyricsManglish ? (
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: 6 }}>
            <Pill
              label="മലയാളം"
              active={effective === "malayalam"}
              onPress={() => {
                setScript("malayalam");
                setPreferences({ script: "malayalam" });
              }}
            />
            <Pill
              label="Manglish"
              active={effective === "manglish"}
              onPress={() => {
                setScript("manglish");
                setPreferences({ script: "manglish" });
              }}
            />
          </View>
        ) : null}
        <View style={{ flexDirection: "row", gap: 6 }}>
          <TextSizeButton
            label="Smaller lyrics"
            title="A−"
            disabled={preferences.fontSize <= 18}
            onPress={() =>
              setPreferences({
                fontSize: Math.max(18, preferences.fontSize - 2),
              })
            }
          />
          <TextSizeButton
            label="Larger lyrics"
            title="A+"
            disabled={preferences.fontSize >= 40}
            onPress={() =>
              setPreferences({
                fontSize: Math.min(40, preferences.fontSize + 2),
              })
            }
          />
        </View>
      </View>
      <View style={{ gap: preferences.fontSize * 0.4 }}>
        {/* Keep verse breaks without rendering full-height empty text lines. */}
        {lyrics.split(/\r?\n\s*\r?\n/).map((verse, index) => (
          <Text
            key={index}
            selectable
            style={{
              color: theme.ink,
              fontFamily: effective === "malayalam" ? "Malayalam" : undefined,
              fontSize: preferences.fontSize,
              lineHeight:
                preferences.fontSize * (effective === "malayalam" ? 1.5 : 1.45),
            }}
          >
            {verse}
          </Text>
        ))}
      </View>
      {!!song.credits && (
        <View style={{ gap: 6 }}>
          <Copy style={{ fontWeight: "600" }}>Credits</Copy>
          <Copy muted>{song.credits}</Copy>
        </View>
      )}
      {song.links?.length > 0 && (
        <View style={{ gap: 10 }}>
          <Copy style={{ fontWeight: "600" }}>Song & karaoke links</Copy>
          {song.links.map((link, index) => (
            <Button
              key={`${link.url}:${index}`}
              title={link.label}
              icon="open-outline"
              variant="outline"
              onPress={() => {
                if (!/^https?:\/\//i.test(link.url)) {
                  setLinkError("This link is unavailable.");
                  return;
                }
                void Linking.openURL(link.url).catch(() =>
                  setLinkError("Unable to open this link."),
                );
              }}
            />
          ))}
        </View>
      )}
      <Notice text={linkError} error />
    </View>
  );
}
export function SongScreen({ id, close }: { id: string; close: () => void }) {
  const { revision, favorites, toggleFavorite, preferences, track } = useApp();
  const theme = useTheme();
  const song = useResource<Song>(`/songs/${id}`, revision);
  const tracked = useRef("");
  const [error, setError] = useState("");
  useEffect(() => {
    if (song.data && tracked.current !== id) {
      tracked.current = id;
      track({ event: "song_open", songId: id });
    }
  }, [id, song.data, track]);
  const awakeGeneration = useRef(0);
  useEffect(() => {
    if (!preferences.keepAwake) return;
    const tag = `lyrics:${id}:${++awakeGeneration.current}`;
    let disposed = false;
    let acquired = false;
    const release = () => {
      void deactivateKeepAwake(tag).catch(() => {});
    };
    void activateKeepAwakeAsync(tag)
      .then(() => {
        acquired = true;
        if (disposed) release();
      })
      .catch(() => {});
    return () => {
      disposed = true;
      if (acquired) release();
    };
  }, [preferences.keepAwake, id]);
  return (
    <View style={{ flex: 1 }}>
      <View
        style={{
          paddingHorizontal: 15,
          paddingVertical: 7,
          borderBottomWidth: 1,
          borderColor: theme.line,
          flexDirection: "row",
          justifyContent: "space-between",
        }}
      >
        <Button
          title="Back"
          icon="arrow-back"
          variant="quiet"
          onPress={close}
        />
        {song.data && (
          <Button
            icon={favorites.includes(id) ? "bookmark" : "bookmark-outline"}
            title={favorites.includes(id) ? "Saved" : "Save"}
            variant="quiet"
            onPress={() => {
              void toggleFavorite(id).catch((error) => setError(error.message));
            }}
          />
        )}
      </View>
      <ScrollView
        contentContainerStyle={{
          padding: 16,
          paddingBottom: 40,
          maxWidth: 640,
          width: "100%",
          alignSelf: "center",
          gap: 16,
        }}
      >
        <Notice text={error} error />
        {song.data ? (
          <SongContent song={song.data} />
        ) : song.loading ? (
          <Loading />
        ) : (
          <Empty
            title="Hymn unavailable"
            detail={
              song.error && song.error !== "not found"
                ? song.error
                : "This hymn may have been withdrawn from the collection."
            }
            retry={song.reload}
          />
        )}
      </ScrollView>
    </View>
  );
}

function TextSizeButton({
  title,
  label,
  disabled,
  onPress,
}: {
  title: string;
  label: string;
  disabled: boolean;
  onPress: () => void;
}) {
  const theme = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled }}
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => ({
        minWidth: 52,
        minHeight: 48,
        paddingVertical: 10,
        paddingHorizontal: 13,
        alignItems: "center",
        justifyContent: "center",
        borderWidth: 1,
        borderColor: theme.line,
        borderRadius: 8,
        opacity: disabled ? 0.4 : pressed ? 0.6 : 1,
      })}
    >
      <Text style={{ fontSize: 17, fontWeight: "500", color: theme.ink }}>
        {title}
      </Text>
    </Pressable>
  );
}
