import { useEffect } from "react";
import { AppState } from "react-native";
import { fetch } from "expo/fetch";
import { API_URL } from "../lib/api";
import { parseEventBlocks } from "../lib/logic";

/** Public invalidations only: every connection reconciles before listening. */
export function useLiveContent(reconcile: () => void) {
  useEffect(() => {
    let disposed = false;
    let active =
      AppState.currentState === "active" || AppState.currentState == null;
    let controller: AbortController | undefined;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let revision = -1;
    let attempt = 0;
    let generation = 0;
    const connect = async () => {
      if (disposed || !active) return;
      const connection = ++generation;
      controller = new AbortController();
      reconcile();
      try {
        const response = await fetch(`${API_URL}/events`, {
          headers: { Accept: "text/event-stream" },
          signal: controller.signal,
        });
        if (!response.ok || !response.body)
          throw new Error("Live connection unavailable");
        attempt = 0;
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        try {
          while (!disposed && active && connection === generation) {
            const chunk = await reader.read();
            if (chunk.done) break;
            buffer += decoder.decode(chunk.value, { stream: true });
            if (buffer.length > 65536) throw new Error("Invalid live event");
            const events = parseEventBlocks(buffer);
            buffer = events.remainder;
            for (const next of events.revisions) {
              if (next !== revision) {
                revision = next;
                reconcile();
              }
            }
          }
        } finally {
          reader.releaseLock();
        }
      } catch {
        /* Catalogue fetches surface connectivity failures; stream retries quietly. */
      }
      if (!disposed && active && connection === generation)
        retry = setTimeout(connect, Math.min(15000, 1000 * 2 ** attempt++));
    };
    const listener = AppState.addEventListener("change", (state) => {
      const nextActive = state === "active";
      if (nextActive === active) return;
      active = nextActive;
      clearTimeout(retry);
      generation++;
      controller?.abort();
      if (active) void connect();
    });
    void connect();
    return () => {
      disposed = true;
      generation++;
      clearTimeout(retry);
      controller?.abort();
      listener.remove();
    };
  }, [reconcile]);
}
