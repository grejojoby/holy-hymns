#!/usr/bin/env python3
"""Fill missing Malayalam/Manglish lyric counterparts for the Grejo collection.

Malayalam → Manglish uses the ml2en algorithm (pip install ml2en).
Manglish → Malayalam uses Google Input Tools phonetic suggestions
(https://inputtools.google.com), which matches common Manglish typing.

Generated text is draft quality and should be reviewed before publishing.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "content" / "lyrics"
CACHE_PATH = Path("/tmp/holy-hymns-transliteration-cache.json")
REVIEW_NOTE = (
    "Generated counterpart script by phonetic transliteration; "
    "review spelling and wording before publishing."
)
MALAYALAM_RE = re.compile(r"[\u0D00-\u0D7F]")
LATIN_RE = re.compile(r"[A-Za-z]")
# Keep markers, punctuation-only lines, and emoji/music symbols untouched.
KEEP_LINE_RE = re.compile(r"^[\s\d\(\)\[\]\{\}\.\,\!\?\:\;\'\"…\-–—/\\|+=*~`@#$%^&_🎵🎼🎤🙏✝️✝️️❤️♥★☆•·]+$")


def fingerprint(entry: dict) -> str:
    fields = [entry[key] for key in (
        "sourceId", "sourceUrl", "title", "titleMalayalam",
        "lyricsMalayalam", "lyricsManglish", "credits",
    )]
    fields.extend([str(len(entry["labels"])), *entry["labels"]])
    fields.append(str(len(entry["links"])))
    for link in entry["links"]:
        fields.extend([link["label"], link["url"]])
    fields.extend([str(len(entry["reviewNotes"])), *entry["reviewNotes"]])
    digest = hashlib.sha256()
    for value in fields:
        data = value.encode("utf-8")
        digest.update(str(len(data)).encode("ascii") + b":" + data)
    return digest.hexdigest()


def load_cache() -> dict[str, str]:
    if CACHE_PATH.is_file():
        return json.loads(CACHE_PATH.read_text(encoding="utf-8"))
    return {}


def save_cache(cache: dict[str, str]) -> None:
    CACHE_PATH.write_text(
        json.dumps(cache, ensure_ascii=False, indent=0, sort_keys=True) + "\n",
        encoding="utf-8",
    )


def google_ml(text: str, retries: int = 5) -> str:
    """Phonetic Manglish/Latin → Malayalam via Google Input Tools."""
    text = text.strip()
    if not text:
        return ""
    # Input Tools occasionally rejects long punctuation runs.
    cleaned = re.sub(r"\.{3,}", "...", text)
    cleaned = re.sub(r"([!?:,;]){2,}", r"\1", cleaned)
    variants = [cleaned]
    if cleaned != text:
        variants.append(text)
    # Final fallback: letters/digits/spaces only.
    stripped = re.sub(r"[^A-Za-z0-9\s\-']+", " ", cleaned)
    stripped = re.sub(r"\s+", " ", stripped).strip()
    if stripped and stripped not in variants:
        variants.append(stripped)

    last_error: Exception | None = None
    for variant in variants:
        query = urllib.parse.urlencode({
            "text": variant,
            "itc": "ml-t-i0-und",
            "num": 1,
            "cp": 0,
            "cs": 1,
            "ie": "utf-8",
            "oe": "utf-8",
            "app": "demopage",
        })
        url = "https://inputtools.google.com/request?" + query
        for attempt in range(retries):
            try:
                with urllib.request.urlopen(url, timeout=30) as response:
                    payload = json.loads(response.read().decode("utf-8"))
                if payload[0] == "SUCCESS" and payload[1] and payload[1][0][1]:
                    return payload[1][0][1][0]
                last_error = RuntimeError(f"Unexpected Google Input Tools response: {payload[:2]}")
                # Non-retryable parse failures → try next variant.
                if payload and payload[0] == "FAILED_TO_PARSE_REQUEST_BODY":
                    break
            except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as exc:
                last_error = exc
                time.sleep(0.4 * (2 ** attempt))
    # Keep the original Latin rather than aborting the whole collection.
    sys.stderr.write(f"warning: leaving Latin phrase unchanged: {text!r} ({last_error})\n")
    return text


def normalize_manglish(text: str) -> str:
    """Light cleanup so phonetic input tools map common hymn spellings better."""
    # Collapse accidental triple+ vowels (Sweeekarikkam → Sweekarikkam).
    return re.sub(r"([aeiouAEIOU])\1{2,}", lambda match: match.group(1) * 2, text)


def transliterate_phrase(phrase: str, cache: dict[str, str]) -> str:
    if not phrase or not LATIN_RE.search(phrase):
        return phrase
    leading = phrase[: len(phrase) - len(phrase.lstrip())]
    trailing = phrase[len(phrase.rstrip()):]
    body = phrase.strip()
    normalized = normalize_manglish(body)
    key = "mg2ml:" + normalized
    if key not in cache:
        # Very long phrases: walk word windows so Google does not drop clauses.
        if len(normalized) > 100:
            words = re.split(r"(\s+)", normalized)
            rebuilt: list[str] = []
            buf = ""
            for part in words:
                if not part:
                    continue
                if not LATIN_RE.search(part):
                    if buf.strip():
                        rebuilt.append(transliterate_phrase(buf, cache))
                        buf = ""
                    rebuilt.append(part)
                    continue
                candidate = buf + part
                if len(candidate) > 80 and buf.strip():
                    rebuilt.append(transliterate_phrase(buf, cache))
                    buf = part
                else:
                    buf = candidate
            if buf:
                rebuilt.append(
                    google_ml(buf.strip()) if buf.strip() and LATIN_RE.search(buf) else buf
                )
            cache[key] = "".join(rebuilt)
        else:
            cache[key] = google_ml(normalized)
    return leading + cache[key] + trailing


def manglish_to_malayalam_line(line: str, cache: dict[str, str]) -> str:
    if not line.strip() or KEEP_LINE_RE.match(line) or not LATIN_RE.search(line):
        return line
    leading = line[: len(line) - len(line.lstrip())]
    trailing = line[len(line.rstrip()):]
    core = line.strip()
    # Split on commas/semicolons so trailing clauses are not dropped.
    pieces = re.split(r"([,;]+)", core)
    rendered: list[str] = []
    for piece in pieces:
        if not piece:
            continue
        if re.fullmatch(r"[,;]+", piece) or not LATIN_RE.search(piece):
            rendered.append(piece)
            continue
        rendered.append(transliterate_phrase(piece, cache))
    return leading + "".join(rendered) + trailing


def collect_prefetch_phrases(text: str) -> set[str]:
    phrases: set[str] = set()
    for line in text.splitlines():
        core = line.strip()
        if not core or not LATIN_RE.search(core) or KEEP_LINE_RE.match(core):
            continue
        for piece in re.split(r"[,;]+", core):
            body = normalize_manglish(piece.strip())
            if body and LATIN_RE.search(body) and len(body) <= 100:
                phrases.add(body)
    return phrases


def manglish_to_malayalam(text: str, cache: dict[str, str]) -> str:
    return "\n".join(manglish_to_malayalam_line(line, cache) for line in text.splitlines())


def malayalam_to_manglish(text: str) -> str:
    from ml2en import ml2en

    return ml2en.transliterate(text)


def counterpart_path(existing: str, language: str) -> str:
    name = Path(existing).name
    return f"{language}/{name}"


def ensure_notes(song: dict) -> None:
    notes = song.setdefault("reviewNotes", [])
    if REVIEW_NOTE not in notes:
        notes.append(REVIEW_NOTE)


def write_index(manifest: dict) -> None:
    rows = [[
        "status", "title", "titleMalayalam", "malayalamFile", "manglishFile",
        "sourceUrl", "reviewNotes", "reviewFile", "reviewReason", "sourceFile",
    ]]

    def add(status: str, song: dict) -> None:
        row = [
            status,
            song.get("title", ""),
            song.get("titleMalayalam", ""),
            song.get("malayalamFile", ""),
            song.get("manglishFile", ""),
            song.get("sourceUrl", ""),
            " | ".join(song.get("reviewNotes", [])),
            song.get("reviewFile", ""),
            song.get("reviewReason", ""),
            song.get("sourceFile", ""),
        ]
        for index, value in enumerate(row):
            trimmed = value.lstrip(" \t\r\n")
            if trimmed and trimmed[0] in "=+-@":
                row[index] = "'" + value
        rows.append(row)

    for song in manifest["songs"]:
        add("review", song)
    for skipped in manifest.get("skipped", []):
        add(skipped.get("reason", "skipped"), skipped)

    with (ROOT / "index.csv").open("w", encoding="utf-8", newline="") as handle:
        csv.writer(handle).writerows(rows)


def process_song(song: dict, cache: dict[str, str], dry_run: bool) -> dict:
    changed = {
        "malayalam_added": False,
        "manglish_added": False,
        "title_malayalam_added": False,
    }
    folder = ROOT
    lyrics_ml = ""
    lyrics_mg = ""
    if song.get("malayalamFile"):
        lyrics_ml = (folder / song["malayalamFile"]).read_text(encoding="utf-8")
    if song.get("manglishFile"):
        lyrics_mg = (folder / song["manglishFile"]).read_text(encoding="utf-8")

    if song.get("manglishFile") and not song.get("malayalamFile"):
        generated = manglish_to_malayalam(lyrics_mg, cache)
        rel = counterpart_path(song["manglishFile"], "malayalam")
        if not dry_run:
            path = folder / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(generated, encoding="utf-8")
            song["malayalamFile"] = rel
            ensure_notes(song)
        lyrics_ml = generated
        changed["malayalam_added"] = True

    if song.get("malayalamFile") and not song.get("manglishFile"):
        # Re-read if we just created malayalam above (shouldn't happen for these).
        if not lyrics_ml and song.get("malayalamFile"):
            lyrics_ml = (folder / song["malayalamFile"]).read_text(encoding="utf-8")
        generated = malayalam_to_manglish(lyrics_ml)
        rel = counterpart_path(song["malayalamFile"], "manglish")
        if not dry_run:
            path = folder / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(generated, encoding="utf-8")
            song["manglishFile"] = rel
            ensure_notes(song)
        lyrics_mg = generated
        changed["manglish_added"] = True

    if not song.get("titleMalayalam", "").strip() and song.get("title", "").strip():
        title = song["title"].strip()
        if MALAYALAM_RE.search(title) and not LATIN_RE.search(title):
            if not dry_run:
                song["titleMalayalam"] = title
                ensure_notes(song)
            changed["title_malayalam_added"] = True
        elif LATIN_RE.search(title):
            generated_title = manglish_to_malayalam_line(title, cache).strip()
            if not dry_run:
                song["titleMalayalam"] = generated_title
                ensure_notes(song)
            changed["title_malayalam_added"] = True

    if not dry_run and any(changed.values()):
        entry = {
            "sourceId": song["sourceId"],
            "sourceUrl": song["sourceUrl"],
            "title": song["title"],
            "titleMalayalam": song.get("titleMalayalam", ""),
            "lyricsMalayalam": lyrics_ml,
            "lyricsManglish": lyrics_mg,
            "credits": song.get("credits", ""),
            "labels": song.get("labels", []),
            "links": song.get("links", []),
            "reviewNotes": song.get("reviewNotes", []),
        }
        song["contentFingerprint"] = fingerprint(entry)

    return changed


def prefetch_unique_lines(songs: list[dict], cache: dict[str, str], workers: int) -> None:
    needed: set[str] = set()
    for song in songs:
        if song.get("manglishFile") and not song.get("malayalamFile"):
            text = (ROOT / song["manglishFile"]).read_text(encoding="utf-8")
            needed.update(collect_prefetch_phrases(text))
        title = song.get("title", "").strip()
        if (
            not song.get("titleMalayalam", "").strip()
            and title
            and LATIN_RE.search(title)
        ):
            needed.update(collect_prefetch_phrases(title))

    needed = {phrase for phrase in needed if "mg2ml:" + phrase not in cache}
    if not needed:
        return

    print(f"Prefetching {len(needed)} unique Manglish phrases…", flush=True)
    done = 0

    def work(line: str) -> tuple[str, str]:
        return line, google_ml(line)

    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [pool.submit(work, line) for line in sorted(needed)]
        for future in as_completed(futures):
            line, result = future.result()
            cache["mg2ml:" + line] = result
            done += 1
            if done % 100 == 0 or done == len(needed):
                save_cache(cache)
                print(f"  cached {done}/{len(needed)}", flush=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--workers", type=int, default=8)
    parser.add_argument("--limit", type=int, default=0, help="Process only N songs (debug)")
    args = parser.parse_args()

    try:
        from ml2en import ml2en  # noqa: F401
    except ImportError:
        print("Install ml2en first: pip install ml2en", file=sys.stderr)
        return 1

    manifest_path = ROOT / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    songs = manifest["songs"]
    cache = load_cache()

    targets = [
        song for song in songs
        if (song.get("manglishFile") and not song.get("malayalamFile"))
        or (song.get("malayalamFile") and not song.get("manglishFile"))
        or not song.get("titleMalayalam", "").strip()
    ]
    if args.limit:
        targets = targets[: args.limit]

    print(f"Songs needing work: {len(targets)}", flush=True)
    prefetch_unique_lines(targets, cache, args.workers)
    save_cache(cache)

    stats = {"malayalam_added": 0, "manglish_added": 0, "title_malayalam_added": 0}
    for index, song in enumerate(targets, 1):
        changed = process_song(song, cache, args.dry_run)
        for key, value in changed.items():
            stats[key] += int(value)
        if index % 25 == 0 or index == len(targets):
            save_cache(cache)
            print(f"Processed {index}/{len(targets)} → {stats}", flush=True)

    if args.dry_run:
        print("Dry run only; no files written.")
        return 0

    save_cache(cache)
    both = sum(1 for song in songs if song.get("malayalamFile") and song.get("manglishFile"))
    ml_count = sum(1 for song in songs if song.get("malayalamFile"))
    mg_count = sum(1 for song in songs if song.get("manglishFile"))
    summary = {
        "sourceCount": 331,
        "songCount": len(songs),
        "skippedCount": len(manifest.get("skipped", [])),
        "malayalamCount": ml_count,
        "manglishCount": mg_count,
        "bothScriptsCount": both,
        "duplicateSourceCount": 0,
        "reviewCount": sum(1 for item in manifest.get("skipped", []) if item.get("reviewFile")),
    }
    # Preserve reviewCount from prior summary if skipped review files differ.
    prior = json.loads((ROOT / "summary.json").read_text(encoding="utf-8"))
    if "reviewCount" in prior:
        summary["reviewCount"] = prior["reviewCount"]

    manifest_path.write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    (ROOT / "summary.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    write_index(manifest)
    print("Updated manifest.json, index.csv, summary.json")
    print(summary)
    print(stats)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
