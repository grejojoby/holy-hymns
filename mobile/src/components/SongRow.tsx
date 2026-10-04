import React from "react";
import { Pressable, Text, View } from "react-native";
import Ionicons from "@expo/vector-icons/Ionicons";
import type { Song } from "../lib/types";
import { Copy, useTheme } from "./ui";

export function SongRow({
  song,
  saved,
  selected = false,
  onPress,
}: {
  song: Song;
  index?: number;
  saved?: boolean;
  selected?: boolean;
  onPress: () => void;
}) {
  const theme = useTheme();
  const primary = song.title || song.titleMalayalam;
  const secondary = song.title && song.titleMalayalam;
  const malayalam = /[\u0D00-\u0D7F]/.test(primary);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ selected }}
      accessibilityLabel={`${primary}${saved ? ", saved" : ""}`}
      onPress={onPress}
      style={({ pressed }) => ({
        minHeight: 64,
        flexDirection: "row",
        alignItems: "center",
        gap: 12,
        paddingVertical: 10,
        borderBottomWidth: 1,
        borderColor: theme.line,
        backgroundColor: pressed || selected ? theme.tint : "transparent",
      })}
    >
      <View style={{ flex: 1, gap: 3 }}>
        <Text
          style={{
            fontFamily: malayalam ? "Malayalam" : undefined,
            fontWeight: malayalam ? "400" : "500",
            color: theme.ink,
            fontSize: 18,
            lineHeight: malayalam ? 29 : 26,
          }}
        >
          {primary}
        </Text>
        {!!secondary && secondary !== primary && (
          <Copy
            muted
            style={{
              fontFamily: /[\u0D00-\u0D7F]/.test(secondary)
                ? "Malayalam"
                : undefined,
              fontSize: 16,
              lineHeight: 26,
            }}
          >
            {secondary}
          </Copy>
        )}
      </View>
      {selected && <Ionicons name="checkmark" size={18} color={theme.accent} />}
      {saved && <Ionicons name="bookmark" size={18} color={theme.accent} />}
      <Ionicons name="chevron-forward" size={18} color={theme.muted} />
    </Pressable>
  );
}
