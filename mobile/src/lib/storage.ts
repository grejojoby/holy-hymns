import AsyncStorage from "@react-native-async-storage/async-storage";
import * as SecureStore from "expo-secure-store";
import { Platform } from "react-native";

const TOKEN_KEY = "holy-hymns-session";
export const tokens = {
  async get(): Promise<string | null> {
    if (Platform.OS === "web")
      return typeof sessionStorage === "undefined"
        ? null
        : sessionStorage.getItem(TOKEN_KEY);
    return SecureStore.getItemAsync(TOKEN_KEY);
  },
  async set(token: string | null) {
    if (Platform.OS === "web") {
      if (typeof sessionStorage !== "undefined")
        token
          ? sessionStorage.setItem(TOKEN_KEY, token)
          : sessionStorage.removeItem(TOKEN_KEY);
      return;
    }
    if (token)
      await SecureStore.setItemAsync(TOKEN_KEY, token, {
        keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
      });
    else await SecureStore.deleteItemAsync(TOKEN_KEY);
  },
};
export async function readLocal<T>(key: string, fallback: T): Promise<T> {
  try {
    const value = await AsyncStorage.getItem(`holy-hymns:${key}`);
    return value ? (JSON.parse(value) as T) : fallback;
  } catch {
    return fallback;
  }
}
export async function writeLocal(key: string, value: unknown) {
  await AsyncStorage.setItem(`holy-hymns:${key}`, JSON.stringify(value));
}
