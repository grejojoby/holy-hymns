import test from "node:test";
import assert from "node:assert/strict";
import {
  availableScript,
  parseLinks,
  parseEventBlocks,
  uniqueIds,
} from "./logic";
test("single-script songs fall back without fabricated text", () => {
  assert.equal(
    availableScript(
      { lyricsMalayalam: "കർത്താവേ", lyricsManglish: "" },
      "manglish",
    ),
    "malayalam",
  );
  assert.equal(
    availableScript(
      { lyricsMalayalam: "", lyricsManglish: "Karthave" },
      "malayalam",
    ),
    "manglish",
  );
});
test("external link validation rejects executable schemes", () => {
  assert.throws(() => parseLinks("Video | javascript:alert(1)"));
  assert.deepEqual(parseLinks("Karaoke | https://example.com/watch"), [
    { label: "Karaoke", url: "https://example.com/watch" },
  ]);
});
test("SSE handles split chunks, CRLF and invalid data", () => {
  const partial = parseEventBlocks('event: content\r\ndata: {"revision":42}');
  assert.deepEqual(partial.revisions, []);
  const complete = parseEventBlocks(
    partial.remainder + "\n\n: ping\n\nevent: content\ndata: broken\n\n",
  );
  assert.deepEqual(complete.revisions, [42]);
  assert.equal(complete.remainder, "");
});
test("favorite merge is idempotent", () => {
  assert.deepEqual(uniqueIds(["a", "b", "a"]), ["a", "b"]);
});

test("withdrawn pending favourite does not block later favourites", async () => {
  const { flushFavoriteQueue } = await import("./logic");
  const sent: string[] = [];
  const acknowledged: [string, boolean][] = [];
  await flushFavoriteQueue(
    { withdrawn: true, available: true },
    async (id) => {
      sent.push(id);
      if (id === "withdrawn") throw { status: 404 };
    },
    async (id, _add, unavailable) => {
      acknowledged.push([id, unavailable]);
    },
  );
  assert.deepEqual(sent, ["withdrawn", "available"]);
  assert.deepEqual(acknowledged, [
    ["withdrawn", true],
    ["available", false],
  ]);
});
test("transient favourite failure leaves queue unacknowledged for retry", async () => {
  const { flushFavoriteQueue } = await import("./logic");
  let acknowledged = false;
  await assert.rejects(
    flushFavoriteQueue(
      { hymn: true },
      async () => {
        throw { status: 503 };
      },
      async () => {
        acknowledged = true;
      },
    ),
  );
  assert.equal(acknowledged, false);
});

test("saved hymn requests are bounded and preserve ordering", async () => {
  const { mapConcurrent } = await import("./logic");
  let active = 0;
  let peak = 0;
  const results = await mapConcurrent([1, 2, 3, 4, 5, 6], 2, async (value) => {
    active++;
    peak = Math.max(peak, active);
    await new Promise((resolve) => setTimeout(resolve, 2));
    active--;
    return value * 2;
  });
  assert.equal(peak, 2);
  assert.deepEqual(results, [2, 4, 6, 8, 10, 12]);
});

test("favorite changes queued during a request are drained, including reversals", async () => {
  const { drainFavoriteQueue } = await import("./logic");
  const pending: Record<string, boolean> = { first: true };
  const sent: [string, boolean][] = [];
  const remaining = await drainFavoriteQueue(
    () => pending,
    async (id, add) => {
      sent.push([id, add]);
      if (sent.length === 1) {
        pending.first = false;
        pending.second = true;
      }
    },
    async (id, add) => {
      if (pending[id] === add) delete pending[id];
    },
  );
  assert.equal(remaining, false);
  assert.deepEqual(sent, [
    ["first", true],
    ["first", false],
    ["second", true],
  ]);
  assert.deepEqual(pending, {});
});
test("failed favorite drain stops without retrying a broken connection", async () => {
  const { drainFavoriteQueue } = await import("./logic");
  let attempts = 0;
  await assert.rejects(
    drainFavoriteQueue(
      () => ({ first: true }),
      async () => {
        attempts++;
        throw new Error("offline");
      },
      async () => {},
    ),
  );
  assert.equal(attempts, 1);
});
test("favorite drain yields after bounded batches if more changes arrive", async () => {
  const { drainFavoriteQueue } = await import("./logic");
  let id = 0;
  const remaining = await drainFavoriteQueue(
    () => ({ [String(id)]: true }),
    async () => {},
    async () => {
      id++;
    },
    2,
  );
  assert.equal(id, 2);
  assert.equal(remaining, true);
});
test("search ignores a hidden alphabet filter and preserves it for browsing", async () => {
  const { catalogueLetter } = await import("./logic");
  assert.equal(catalogueLetter("Karthave", "A"), undefined);
  assert.equal(catalogueLetter("കർത്താവേ", "അ"), undefined);
  assert.equal(catalogueLetter("", "അ"), "അ");
});
