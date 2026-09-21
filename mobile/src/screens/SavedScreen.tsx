import React, { useEffect, useState } from "react";
import { View } from "react-native";
import { useApp } from "../hooks/AppContext";
import { api, ApiError, message } from "../lib/api";
import { mapConcurrent } from "../lib/logic";
import type { Song } from "../lib/types";
import { Button, Empty, Loading, Notice, Page } from "../components/ui";
import { SongRow } from "../components/SongRow";
export function SavedScreen({ openSong }: { openSong: (id: string) => void }) {
  const { favorites, revision, syncError, syncFavorites } = useApp();
  const [songs, setSongs] = useState<Song[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    mapConcurrent(favorites, 4, (id) =>
      api<Song>(`/songs/${id}`, { signal: controller.signal }).catch(
        (error) => {
          if (error instanceof ApiError && error.status === 404) return null;
          throw error;
        },
      ),
    )
      .then((items) => {
        if (!controller.signal.aborted)
          setSongs(items.filter((song): song is Song => !!song));
      })
      .catch((error) => {
        if (!controller.signal.aborted) {
          setSongs([]);
          setError(message(error));
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [favorites.join(","), revision, retry]);
  return (
    <Page>
      <Notice
        text={
          syncError ? `Saved on this device. Sync is waiting: ${syncError}` : ""
        }
        error
      />
      {!!syncError && (
        <Button
          title="Retry sync"
          onPress={() => {
            void syncFavorites();
          }}
          variant="outline"
        />
      )}
      {loading ? (
        <Loading />
      ) : error ? (
        <Empty
          title="Unable to load saved hymns"
          detail={error}
          retry={() => setRetry((value) => value + 1)}
        />
      ) : songs.length ? (
        <View>
          {songs.map((song) => (
            <SongRow
              key={song.id}
              song={song}
              saved
              onPress={() => openSong(song.id)}
            />
          ))}
        </View>
      ) : (
        <Empty
          title="No saved hymns"
          detail="Tap Save on a hymn to find it here."
        />
      )}
    </Page>
  );
}
