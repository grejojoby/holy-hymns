import type { Song, Preferences } from "./types";
export const defaultPreferences: Preferences = {
  darkMode: "system",
  fontSize: 20,
  script: "malayalam",
  analytics: false,
  keepAwake: false,
};
export function availableScript(
  song: Pick<Song, "lyricsMalayalam" | "lyricsManglish">,
  preferred: Preferences["script"],
) {
  if (preferred === "malayalam" && song.lyricsMalayalam) return "malayalam";
  if (preferred === "manglish" && song.lyricsManglish) return "manglish";
  return song.lyricsMalayalam ? "malayalam" : "manglish";
}
export function parseLinks(value: string) {
  return value
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const separator = line.indexOf("|");
      const url = (separator < 0 ? line : line.slice(separator + 1)).trim();
      const label =
        (separator < 0 ? "Listen" : line.slice(0, separator)).trim() ||
        "Listen";
      if (!/^https?:\/\//i.test(url))
        throw new Error("External links must begin with https:// or http://.");
      return { label, url };
    });
}
export function uniqueIds(ids: string[]) {
  return [...new Set(ids)];
}
export function parseEventBlocks(buffer: string): {
  remainder: string;
  revisions: number[];
} {
  const normalized = buffer.replace(/\r\n/g, "\n");
  const blocks = normalized.split("\n\n");
  const remainder = blocks.pop() || "";
  const revisions: number[] = [];
  for (const block of blocks) {
    if (!block.split("\n").some((line) => line === "event: content")) continue;
    const data = block
      .split("\n")
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice(5).trim())
      .join("\n");
    try {
      const parsed = JSON.parse(data);
      if (Number.isSafeInteger(parsed.revision) && parsed.revision >= 0)
        revisions.push(parsed.revision);
    } catch {
      /* Ignore heartbeat and malformed events. */
    }
  }
  return { remainder, revisions };
}

/** A withdrawn song must not prevent unrelated favourites from synchronizing. */
export async function flushFavoriteQueue(
  pending: Record<string, boolean>,
  send: (id: string, add: boolean) => Promise<unknown>,
  acknowledge: (
    id: string,
    add: boolean,
    unavailable: boolean,
  ) => Promise<void>,
) {
  for (const [id, add] of Object.entries(pending)) {
    let unavailable = false;
    try {
      await send(id, add);
    } catch (error) {
      if (
        typeof error === "object" &&
        error !== null &&
        "status" in error &&
        error.status === 404
      )
        unavailable = true;
      else throw error;
    }
    await acknowledge(id, add, unavailable);
  }
}

export async function mapConcurrent<T, R>(
  items: T[],
  limit: number,
  transform: (item: T) => Promise<R>,
): Promise<R[]> {
  const results = new Array<R>(items.length);
  let cursor = 0;
  await Promise.all(
    Array.from(
      { length: Math.min(Math.max(1, limit), items.length) },
      async () => {
        while (cursor < items.length) {
          const index = cursor++;
          results[index] = await transform(items[index]!);
        }
      },
    ),
  );
  return results;
}

/** Drain additions and reversals that arrive while a previous batch is in flight. */
export async function drainFavoriteQueue(
  readPending: () => Record<string, boolean>,
  send: (id: string, add: boolean) => Promise<unknown>,
  acknowledge: (
    id: string,
    add: boolean,
    unavailable: boolean,
  ) => Promise<void>,
  maxBatches = 8,
): Promise<boolean> {
  for (let batch = 0; batch < maxBatches; batch++) {
    const pending = Object.fromEntries(
      Object.entries(readPending()).slice(0, 50),
    );
    if (!Object.keys(pending).length) return false;
    await flushFavoriteQueue(pending, send, acknowledge);
  }
  return Object.keys(readPending()).length > 0;
}

export function catalogueLetter(search: string, selectedLetter: string) {
  return search.trim() ? undefined : selectedLetter;
}
