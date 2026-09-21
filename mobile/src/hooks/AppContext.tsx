import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { AppState, Platform } from "react-native";
import * as AppleAuthentication from "expo-apple-authentication";
import {
  api,
  ApiError,
  message,
  post,
  setSessionToken,
  setUnauthorizedHandler,
} from "../lib/api";
import {
  defaultPreferences,
  drainFavoriteQueue,
  uniqueIds,
} from "../lib/logic";
import { readLocal, tokens, writeLocal } from "../lib/storage";
import type {
  AnalyticsEvent,
  AuthResult,
  List,
  Preferences,
  User,
} from "../lib/types";
import { useLiveContent } from "./useLiveContent";

type FavoriteData = { ids: string[]; pending: Record<string, boolean> };
type Context = {
  ready: boolean;
  user: User | null;
  preferences: Preferences;
  revision: number;
  favorites: string[];
  syncError: string;
  syncFavorites: () => Promise<void>;
  setPreferences: (changes: Partial<Preferences>) => void;
  acceptSession: (result: AuthResult) => Promise<void>;
  logout: (all?: boolean) => Promise<void>;
  clearSession: () => Promise<void>;
  toggleFavorite: (id: string) => Promise<void>;
  track: (event: AnalyticsEvent) => void;
  invalidate: () => void;
};
const AppContext = createContext<Context | null>(null);
export function useApp() {
  const value = useContext(AppContext);
  if (!value) throw new Error("Missing AppProvider");
  return value;
}
export function AppProvider({ children }: React.PropsWithChildren) {
  const [ready, setReady] = useState(false);
  const [user, setUser] = useState<User | null>(null);
  const [preferences, setPreferencesState] =
    useState<Preferences>(defaultPreferences);
  const [revision, setRevision] = useState(0);
  const [favorites, setFavorites] = useState<string[]>([]);
  const [syncError, setSyncError] = useState("");
  const favoriteState = useRef<FavoriteData>({ ids: [], pending: {} });
  const favoriteKey = useRef("favorites:guest");
  const currentUser = useRef<User | null>(null);
  const syncing = useRef(false);
  const syncRetry = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  useEffect(() => () => clearTimeout(syncRetry.current), []);
  const invalidate = useCallback(() => setRevision((value) => value + 1), []);
  useLiveContent(invalidate);
  const clearSession = useCallback(async () => {
    clearTimeout(syncRetry.current);
    setSessionToken(null);
    currentUser.current = null;
    setUser(null);
    setFavorites([]);
    favoriteState.current = { ids: [], pending: {} };
    await tokens.set(null);
  }, []);
  useEffect(() => {
    setUnauthorizedHandler(() => {
      void clearSession();
    });
  }, [clearSession]);
  useEffect(() => {
    if (Platform.OS !== "ios") return;
    const subscription = AppleAuthentication.addRevokeListener(() => {
      void post("/auth/logout")
        .catch(() => {})
        .finally(() => {
          void clearSession();
        });
    });
    return () => subscription.remove();
  }, [clearSession]);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const stored = await readLocal("preferences", defaultPreferences);
      setPreferencesState({
        ...defaultPreferences,
        ...stored,
        fontSize: Math.max(18, Math.min(40, stored.fontSize || 20)),
      });
      try {
        const token = await tokens.get();
        if (token) {
          setSessionToken(token);
          const account = await api<User>("/auth/me");
          if (!cancelled) {
            currentUser.current = account;
            setUser(account);
          }
        }
      } catch (error) {
        if (error instanceof ApiError && error.status === 401)
          await clearSession();
        // Failed validation never grants cached administrative access.
      } finally {
        if (!cancelled) setReady(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [clearSession]);
  const persistFavorites = useCallback(async (next: FavoriteData) => {
    favoriteState.current = next;
    setFavorites(next.ids);
    await writeLocal(favoriteKey.current, next);
  }, []);
  const syncFavorites: () => Promise<void> = useCallback(async () => {
    if (!currentUser.current || syncing.current) return;
    const accountID = currentUser.current.id;
    syncing.current = true;
    clearTimeout(syncRetry.current);
    let morePending = false;
    try {
      const remote = await api<List<string>>("/me/favorites");
      if (currentUser.current?.id !== accountID) return;
      const pending = { ...favoriteState.current.pending };
      const merged = new Set(remote.items || []);
      Object.entries(pending).forEach(([id, add]) =>
        add ? merged.add(id) : merged.delete(id),
      );
      await persistFavorites({
        ids: [...merged],
        pending: favoriteState.current.pending,
      });
      morePending = await drainFavoriteQueue(
        () =>
          currentUser.current?.id === accountID
            ? favoriteState.current.pending
            : {},
        async (id, add) => {
          if (currentUser.current?.id !== accountID)
            throw new Error("Account changed.");
          await api(`/me/favorites/${id}`, { method: add ? "PUT" : "DELETE" });
        },
        async (id, add, unavailable) => {
          if (currentUser.current?.id !== accountID) return;
          if (favoriteState.current.pending[id] === add || unavailable) {
            const rest = { ...favoriteState.current.pending };
            delete rest[id];
            await persistFavorites({
              ids: unavailable
                ? favoriteState.current.ids.filter((value) => value !== id)
                : favoriteState.current.ids,
              pending: rest,
            });
          }
        },
      );
      setSyncError("");
    } catch (error) {
      setSyncError(message(error));
    } finally {
      syncing.current = false;
      if (morePending && currentUser.current?.id === accountID) {
        syncRetry.current = setTimeout(() => {
          void syncFavorites();
        }, 250);
      }
    }
  }, [persistFavorites]);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      favoriteKey.current = `favorites:${user?.id || "guest"}`;
      setFavorites([]);
      favoriteState.current = { ids: [], pending: {} };
      const saved = await readLocal<FavoriteData>(favoriteKey.current, {
        ids: [],
        pending: {},
      });
      if (cancelled) return;
      favoriteState.current = saved;
      setFavorites(saved.ids);
      setSyncError("");
      if (user) await syncFavorites();
    })();
    return () => {
      cancelled = true;
    };
  }, [user?.id, syncFavorites]);
  useEffect(() => {
    const listener = AppState.addEventListener("change", (state) => {
      if (state === "active") {
        void syncFavorites();
        if (currentUser.current)
          void api<User>("/auth/me")
            .then((account) => {
              currentUser.current = account;
              setUser(account);
            })
            .catch(() => {});
      }
    });
    return () => listener.remove();
  }, [syncFavorites]);
  const acceptSession = async (result: AuthResult) => {
    await tokens.set(result.token);
    setSessionToken(result.token);
    const guest = await readLocal<FavoriteData>("favorites:guest", {
      ids: [],
      pending: {},
    });
    const key = `favorites:${result.user.id}`;
    const saved = await readLocal<FavoriteData>(key, { ids: [], pending: {} });
    const next = {
      ids: uniqueIds([...saved.ids, ...guest.ids]),
      pending: { ...saved.pending },
    };
    guest.ids.forEach((id) => {
      next.pending[id] = true;
    });
    await writeLocal(key, next);
    await writeLocal("favorites:guest", { ids: [], pending: {} });
    currentUser.current = result.user;
    setUser(result.user);
  };
  const logout = async (all = false) => {
    await post(all ? "/auth/logout-all" : "/auth/logout");
    await clearSession();
  };
  const setPreferences = (changes: Partial<Preferences>) =>
    setPreferencesState((previous) => {
      const next = { ...previous, ...changes };
      void writeLocal("preferences", next);
      return next;
    });
  const track = useCallback(
    (event: AnalyticsEvent) => {
      if (preferences.analytics && (!user || user.role === "reader"))
        void post("/analytics", event).catch(() => {});
    },
    [preferences.analytics, user],
  );
  const toggleFavorite = async (id: string) => {
    const state = favoriteState.current;
    const add = !state.ids.includes(id);
    await persistFavorites({
      ids: add ? [...state.ids, id] : state.ids.filter((value) => value !== id),
      pending: user ? { ...state.pending, [id]: add } : {},
    });
    if (add) track({ event: "favorite", songId: id });
    await syncFavorites();
  };
  return (
    <AppContext.Provider
      value={{
        ready,
        user,
        preferences,
        revision,
        favorites,
        syncError,
        syncFavorites,
        setPreferences,
        acceptSession,
        logout,
        clearSession,
        toggleFavorite,
        track,
        invalidate,
      }}
    >
      {children}
    </AppContext.Provider>
  );
}
