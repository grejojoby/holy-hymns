import React from "react";
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  useColorScheme,
  View,
  type TextInputProps,
  type ViewStyle,
} from "react-native";
import Ionicons from "@expo/vector-icons/Ionicons";
import { useApp } from "../hooks/AppContext";

export function useTheme() {
  const { preferences } = useApp();
  const system = useColorScheme();
  const dark =
    preferences.darkMode === "dark" ||
    (preferences.darkMode === "system" && system === "dark");
  return {
    dark,
    paper: dark ? "#171A18" : "#FAFAF8",
    surface: dark ? "#202521" : "#FFFFFF",
    ink: dark ? "#F2F4F1" : "#242925",
    muted: dark ? "#B7C0B9" : "#59645C",
    line: dark ? "#3D493F" : "#D8DED9",
    controlBorder: dark ? "#829188" : "#7B897F",
    accent: dark ? "#A8CEB2" : "#38614C",
    tint: dark ? "#2C3930" : "#EEF3EF",
    error: dark ? "#FFB4A4" : "#9B332B",
  };
}
export function Copy({
  children,
  muted = false,
  style,
}: React.PropsWithChildren<{ muted?: boolean; style?: any }>) {
  const theme = useTheme();
  return (
    <Text
      style={[styles.copy, { color: muted ? theme.muted : theme.ink }, style]}
    >
      {children}
    </Text>
  );
}
export function Heading({
  children,
  level = 1,
}: React.PropsWithChildren<{ level?: 1 | 2 | 3 }>) {
  const theme = useTheme();
  return (
    <Text
      accessibilityRole="header"
      style={{
        fontWeight: "600",
        color: theme.ink,
        fontSize: level === 1 ? 20 : level === 2 ? 18 : 16,
        lineHeight: level === 1 ? 28 : level === 2 ? 26 : 24,
      }}
    >
      {children}
    </Text>
  );
}
export function Eyebrow({ children }: React.PropsWithChildren) {
  const theme = useTheme();
  return (
    <Text
      style={{
        color: theme.muted,
        fontWeight: "600",
        fontSize: 14,
        lineHeight: 22,
        marginBottom: 8,
      }}
    >
      {children}
    </Text>
  );
}
export function Button({
  title,
  onPress,
  variant = "solid",
  busy = false,
  disabled = false,
  icon,
  small = false,
}: {
  title: string;
  onPress: () => void;
  variant?: "solid" | "outline" | "quiet" | "danger";
  busy?: boolean;
  disabled?: boolean;
  icon?: React.ComponentProps<typeof Ionicons>["name"];
  small?: boolean;
}) {
  const theme = useTheme();
  const solid = variant === "solid";
  const color =
    variant === "danger" ? theme.error : solid ? theme.paper : theme.ink;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={title}
      accessibilityState={{ disabled: disabled || busy, busy }}
      onPress={onPress}
      disabled={disabled || busy}
      style={({ pressed }) => [
        {
          minHeight: 48,
          paddingVertical: 10,
          paddingHorizontal: small ? 12 : 16,
          borderRadius: 8,
          backgroundColor: solid ? theme.accent : "transparent",
          borderWidth: variant === "outline" ? 1 : 0,
          borderColor: theme.line,
          flexDirection: "row",
          alignItems: "center",
          justifyContent: "center",
          gap: 9,
          opacity: disabled || busy ? 0.5 : pressed ? 0.75 : 1,
        },
        small && { alignSelf: "flex-start" },
      ]}
    >
      {busy ? (
        <ActivityIndicator color={color} size="small" />
      ) : icon ? (
        <Ionicons name={icon} size={18} color={color} />
      ) : null}
      <Text
        style={{
          color,
          fontWeight: "600",
          fontSize: 14,
          flexShrink: 1,
          textAlign: "center",
        }}
      >
        {title}
      </Text>
    </Pressable>
  );
}
export function IconButton({
  label,
  icon,
  onPress,
  active = false,
}: {
  label: string;
  icon: React.ComponentProps<typeof Ionicons>["name"];
  onPress: () => void;
  active?: boolean;
}) {
  const theme = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      style={({ pressed }) => ({
        minHeight: 48,
        minWidth: 48,
        borderRadius: 8,
        alignItems: "center",
        justifyContent: "center",
        backgroundColor: active ? theme.tint : "transparent",
        opacity: pressed ? 0.6 : 1,
      })}
    >
      <Ionicons name={icon} size={22} color={theme.ink} />
    </Pressable>
  );
}
export function Field({
  label,
  hideLabel = false,
  ...props
}: TextInputProps & { label: string; hideLabel?: boolean }) {
  const theme = useTheme();
  return (
    <View style={{ gap: 8 }}>
      {!hideLabel && (
        <Copy style={{ fontWeight: "500", fontSize: 14 }}>{label}</Copy>
      )}
      <TextInput
        {...props}
        accessibilityLabel={label}
        placeholderTextColor={theme.muted}
        style={[
          {
            color: theme.ink,
            backgroundColor: theme.surface,
            borderColor: theme.controlBorder,
            borderWidth: 1,
            borderRadius: 8,
            minHeight: 48,
            padding: 12,
            fontSize: 14,
            textAlignVertical: props.multiline ? "top" : "center",
          },
          props.multiline && { minHeight: 120 },
          props.style,
        ]}
      />
    </View>
  );
}
export function Pill({
  label,
  active = false,
  onPress,
}: {
  label: string;
  active?: boolean;
  onPress: () => void;
}) {
  const theme = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ selected: active }}
      onPress={onPress}
      style={({ pressed }) => ({
        minHeight: 48,
        paddingHorizontal: 14,
        paddingVertical: 10,
        borderRadius: 8,
        borderWidth: 1,
        borderColor: active ? theme.accent : theme.line,
        backgroundColor: active ? theme.tint : "transparent",
        opacity: pressed ? 0.7 : 1,
      })}
    >
      <Text
        style={{
          fontFamily: /[\u0D00-\u0D7F]/.test(label) ? "Malayalam" : undefined,
          fontWeight: "500",
          fontSize: 13,
          lineHeight: 24,
          color: active ? theme.accent : theme.ink,
        }}
      >
        {label}
      </Text>
    </Pressable>
  );
}
export function Panel({
  children,
  style,
}: React.PropsWithChildren<{ style?: ViewStyle }>) {
  const theme = useTheme();
  return (
    <View
      style={[
        {
          backgroundColor: theme.surface,
          borderWidth: 1,
          borderColor: theme.line,
          padding: 14,
          borderRadius: 8,
          gap: 16,
        },
        style,
      ]}
    >
      {children}
    </View>
  );
}
export function Notice({
  text,
  error = false,
}: {
  text?: string;
  error?: boolean;
}) {
  const theme = useTheme();
  return text ? (
    <View
      accessibilityLiveRegion="polite"
      style={{
        borderLeftWidth: 3,
        borderColor: error ? theme.error : theme.accent,
        padding: 13,
        backgroundColor: theme.tint,
        borderRadius: 4,
      }}
    >
      <Copy style={{ color: error ? theme.error : theme.ink, fontSize: 14 }}>
        {text}
      </Copy>
    </View>
  ) : null;
}
export function Empty({
  title,
  detail,
  retry,
}: {
  title: string;
  detail: string;
  retry?: () => void;
}) {
  return (
    <View style={{ paddingVertical: 20, gap: 8, alignItems: "flex-start" }}>
      <Heading level={2}>{title}</Heading>
      <Copy muted>{detail}</Copy>
      {retry && <Button title="Try again" onPress={retry} variant="outline" />}
    </View>
  );
}
export function Loading() {
  const theme = useTheme();
  return (
    <ActivityIndicator
      style={{ padding: 30 }}
      color={theme.accent}
      accessibilityLabel="Loading"
    />
  );
}
export function Toggle({
  title,
  description,
  value,
  onChange,
}: {
  title: string;
  description?: string;
  value: boolean;
  onChange: (value: boolean) => void;
}) {
  const theme = useTheme();
  return (
    <Pressable
      accessibilityRole="switch"
      accessibilityLabel={title}
      accessibilityHint={description}
      accessibilityState={{ checked: value }}
      onPress={() => onChange(!value)}
      style={{
        flexDirection: "row",
        alignItems: "center",
        gap: 16,
        minHeight: 48,
      }}
    >
      <View style={{ flex: 1, gap: 4 }}>
        <Copy>{title}</Copy>
        {!!description && (
          <Copy muted style={{ fontSize: 13 }}>
            {description}
          </Copy>
        )}
      </View>
      <View
        pointerEvents="none"
        aria-hidden
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
      >
        <Switch
          accessible={false}
          focusable={false}
          tabIndex={-1}
          value={value}
          trackColor={{ true: theme.accent }}
        />
      </View>
    </Pressable>
  );
}
export function Page({ children }: React.PropsWithChildren) {
  return (
    <ScrollView
      keyboardShouldPersistTaps="handled"
      automaticallyAdjustKeyboardInsets
      keyboardDismissMode="interactive"
      contentContainerStyle={{
        padding: 16,
        paddingBottom: 32,
        gap: 16,
        width: "100%",
        maxWidth: 680,
        alignSelf: "center",
      }}
    >
      {children}
    </ScrollView>
  );
}
export const styles = StyleSheet.create({
  copy: { fontSize: 14, lineHeight: 22 },
  row: { flexDirection: "row", alignItems: "center", gap: 12 },
  stack: { gap: 16 },
});
