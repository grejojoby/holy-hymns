#!/usr/bin/env python3
"""Validate an extracted lyrics folder and upload unpublished drafts. Python 3.10+."""

import argparse
import getpass
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


SOURCE_ID = re.compile(r"tag:blogger\.com,1999:blog-6936100729546217639\.post-[0-9]{1,32}\Z")
SHA256 = re.compile(r"[0-9a-fA-F]{64}\Z")
DEFAULT_FOLDER = Path(__file__).resolve().parents[1] / "content" / "lyrics"
MAX_FILE_BYTES = 200_000


class UploadError(Exception):
    pass


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise UploadError(f"Duplicate JSON key: {key}")
        result[key] = value
    return result


def text_field(obj, name, limit, required=False):
    value = obj.get(name, "")
    if not isinstance(value, str) or "\0" in value or len(value) > limit:
        raise UploadError(f"Invalid or oversized {name}")
    if required and not value.strip():
        raise UploadError(f"Missing {name}")
    return value


def strings_field(obj, name, count, length):
    values = obj.get(name, [])
    if not isinstance(values, list) or len(values) > count:
        raise UploadError(f"Invalid {name} list")
    for value in values:
        text_field({name: value}, name, length, required=True)
    return values


def public_https_url(value, source=False):
    try:
        url = urllib.parse.urlsplit(value)
        if len(value.encode("utf-8")) > 2048 or any(ord(char) < 32 for char in value):
            raise ValueError()
        _ = url.port
        valid = url.scheme == "https" and bool(url.hostname) and url.username is None and url.password is None
        if source:
            valid = valid and url.netloc == "grejolyrics.blogspot.com" and "?" not in value
            valid = valid and bool(re.fullmatch(r"/[0-9]{4}/(0[1-9]|1[0-2])/[A-Za-z0-9_-][A-Za-z0-9._~-]*\.html", url.path)) and not url.query and not url.fragment
        if not valid:
            raise ValueError()
    except ValueError:
        raise UploadError("Invalid source URL" if source else "External links must use HTTPS") from None


def fingerprint(entry):
    """Length-prefixed UTF-8 fields, shared with the Go bundle exporter."""
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


def read_lyrics(folder, relative, language):
    if relative == "":
        return ""
    if not isinstance(relative, str) or "\\" in relative:
        raise UploadError("Invalid lyric file path")
    path = Path(relative)
    if path.is_absolute() or len(path.parts) != 2 or path.parts[0] != language or path.suffix != ".txt":
        raise UploadError(f"Lyrics must be a .txt file directly inside {language}/")
    resolved = (folder / path).resolve()
    if not resolved.is_relative_to(folder) or not resolved.is_file():
        raise UploadError(f"Missing or unsafe lyric file: {relative}")
    if resolved.stat().st_size > MAX_FILE_BYTES:
        raise UploadError(f"Lyric file exceeds {MAX_FILE_BYTES} bytes: {relative}")
    # Preserve stanza breaks and Unicode; normalise editor-created CRLF only.
    return resolved.read_text(encoding="utf-8")


def load_bundle(folder):
    folder = Path(folder).resolve()
    manifest_path = folder / "manifest.json"
    if not manifest_path.is_file() or manifest_path.stat().st_size > 20 * 1024 * 1024:
        raise UploadError("Missing manifest.json or manifest exceeds 20 MiB")
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
    if not isinstance(manifest, dict) or manifest.get("schemaVersion") != 1:
        raise UploadError("Unsupported lyrics manifest schema")
    songs = manifest.get("songs")
    if not isinstance(songs, list) or not songs or len(songs) > 10000:
        raise UploadError("Manifest must contain between 1 and 10,000 songs")
    entries, seen, edited = [], set(), 0
    for song in songs:
        if not isinstance(song, dict):
            raise UploadError("Invalid manifest song")
        entry = {key: text_field(song, key, limit) for key, limit in (
            ("sourceId", 200), ("sourceUrl", 2048), ("title", 500),
            ("titleMalayalam", 500), ("credits", 4000),
        )}
        if not SOURCE_ID.fullmatch(entry["sourceId"]) or entry["sourceId"] in seen:
            raise UploadError("Missing, invalid or duplicate Blogger source ID")
        seen.add(entry["sourceId"])
        public_https_url(entry["sourceUrl"], source=True)
        if not (entry["title"].strip() or entry["titleMalayalam"].strip()):
            raise UploadError(f"Missing title for {entry['sourceId']}")
        entry["labels"] = strings_field(song, "labels", 30, 120)
        if any("\n" in label or "\r" in label for label in entry["labels"]):
            raise UploadError("Labels cannot contain line breaks")
        entry["reviewNotes"] = strings_field(song, "reviewNotes", 30, 1000)
        links = song.get("links", [])
        if not isinstance(links, list) or len(links) > 10:
            raise UploadError("Invalid external links")
        entry["links"] = []
        for link in links:
            if not isinstance(link, dict):
                raise UploadError("Invalid external link")
            value = {key: text_field(link, key, limit, required=True) for key, limit in (("label", 120), ("url", 2048))}
            public_https_url(value["url"])
            entry["links"].append(value)
        for key, path_key, directory in (
            ("lyricsMalayalam", "malayalamFile", "malayalam"),
            ("lyricsManglish", "manglishFile", "manglish"),
        ):
            entry[key] = read_lyrics(folder, song.get(path_key, ""), directory)
            if "\0" in entry[key]:
                raise UploadError(f"Null character in {path_key}")
        if not (entry["lyricsMalayalam"].strip() or entry["lyricsManglish"].strip()):
            raise UploadError(f"No lyrics for {entry['sourceId']}")
        if len((entry["lyricsMalayalam"] + entry["lyricsManglish"]).encode("utf-8")) > MAX_FILE_BYTES:
            raise UploadError(f"Combined lyrics exceed {MAX_FILE_BYTES} bytes")
        source_hash = song.get("sourceHash", "")
        baseline = song.get("contentFingerprint", "")
        if not isinstance(source_hash, str) or not SHA256.fullmatch(source_hash) or not isinstance(baseline, str) or not SHA256.fullmatch(baseline):
            raise UploadError("Missing source hash or content fingerprint")
        current = fingerprint(entry)
        changed = current != baseline.lower()
        edited += changed
        entry["hash"] = hashlib.sha256((source_hash.lower() + ":" + current).encode("ascii")).hexdigest() if changed else source_hash.lower()
        entries.append(entry)
    return entries, edited


def api_base(value):
    try:
        url = urllib.parse.urlsplit(value)
        local = url.hostname == "localhost"
        try:
            local = local or ipaddress.ip_address(url.hostname or "").is_loopback
        except ValueError:
            pass
        if not url.hostname or url.username is not None or url.password is not None or url.query or url.fragment:
            raise ValueError()
        if url.scheme != "https" and not (url.scheme == "http" and local):
            raise ValueError()
        _ = url.port
    except ValueError:
        raise UploadError("Use an HTTPS API URL (HTTP is allowed only on localhost/loopback)") from None
    base = value.rstrip("/")
    return base if url.path.rstrip("/").endswith("/v1") else base + "/v1"


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Client:
    def __init__(self, base):
        self.base = api_base(base)
        self.opener = urllib.request.build_opener(NoRedirect)

    def post(self, route, data, token="", retry=False):
        if token and (not isinstance(token, str) or len(token) > 4096 or not re.fullmatch(r"[A-Za-z0-9._~-]+", token)):
            raise UploadError("Invalid session token format")
        payload = json.dumps(data, ensure_ascii=False).encode("utf-8")
        for attempt in range(3 if retry else 1):
            headers = {"Content-Type": "application/json", "User-Agent": "HolyHymns-Lyrics-Uploader/1"}
            if token:
                headers["Authorization"] = "Bearer " + token
            request = urllib.request.Request(self.base + route, data=payload, headers=headers, method="POST")
            try:
                with self.opener.open(request, timeout=30) as response:
                    body = response.read(1 << 20)
                    return json.loads(body) if body else {}
            except urllib.error.HTTPError as error:
                if retry and attempt < 2 and error.code in (429, 502, 503, 504):
                    delay = error.headers.get("Retry-After", "")
                    error.close()
                    if delay.isdigit() and int(delay) > 10:
                        raise UploadError(f"Server asked for a longer retry; rerun later (HTTP {error.code})") from None
                    time.sleep(max(1, int(delay)) if delay.isdigit() else attempt + 1)
                    continue
                code = error.code
                error.close()
                # Never echo untrusted HTTP bodies containing credentials or private data.
                hints = {401: "sign in again", 403: "an active administrator account is required", 404: "deploy the backend version containing the lyrics-import endpoint", 409: "review the server conflict", 429: "retry later"}
                raise UploadError(f"HTTP {code}: {hints.get(code, 'request rejected; check server logs')} at {route}") from None
            except (urllib.error.URLError, TimeoutError, OSError):
                if retry and attempt < 2:
                    time.sleep(attempt + 1)
                    continue
                raise UploadError(f"Connection failed at {route}; rerunning imports is safe") from None
        raise UploadError("Retry limit reached")


def upload(client, entries, token, dry_run=False):
    totals = dict(total=0, created=0, unchanged=0, conflicts=0, skipped=0)
    issues = []
    for index, entry in enumerate(entries, 1):
        try:
            result = client.post("/admin/imports/lyrics", {"entry": entry, "dryRun": dry_run}, token, retry=True)
        except UploadError as error:
            raise UploadError(f"Stopped at {entry['sourceId']} after {index - 1} completed requests. {error}") from None
        for key in totals:
            value = result.get(key)
            if not isinstance(value, int) or value < 0:
                raise UploadError("Unexpected import response; check server version")
            totals[key] += value
        if result["conflicts"] or result["skipped"]:
            issues.append({"sourceId": entry["sourceId"], "title": entry["title"], "conflicts": result["conflicts"], "skipped": result["skipped"], "review": result.get("review", [])})
        if index % 25 == 0 or index == len(entries):
            print(f"Checked {index}/{len(entries)}" if dry_run else f"Uploaded {index}/{len(entries)}", file=sys.stderr)
    return {"dryRun": dry_run, **totals, "issues": issues}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--folder", type=Path, default=DEFAULT_FOLDER, help="extracted folder containing manifest.json")
    parser.add_argument("--api-url", help="server origin or versioned API URL, e.g. https://hymns.example.com/v1")
    parser.add_argument("--email", default=os.getenv("HOLY_HYMNS_UPLOAD_EMAIL", ""), help="administrator email (password is prompted securely)")
    parser.add_argument("--validate-only", action="store_true", help="validate local files without contacting a server")
    parser.add_argument("--dry-run", action="store_true", help="check against server without importing")
    parser.add_argument("--source-id", action="append", help="upload only these Blogger source IDs; repeat to select multiple")
    parser.add_argument("--report", type=Path, help="optional JSON results file")
    args = parser.parse_args(argv)
    try:
        entries, edited = load_bundle(args.folder)
        print(f"Validated {len(entries)} songs; {edited} changed since extraction.", file=sys.stderr)
        if args.source_id:
            wanted = set(args.source_id)
            if not wanted.issubset({entry["sourceId"] for entry in entries}):
                raise UploadError("A requested --source-id is not in the manifest")
            entries = [entry for entry in entries if entry["sourceId"] in wanted]
        if args.validate_only:
            return 0
        if not args.api_url:
            raise UploadError("Provide --api-url or use --validate-only")
        client = Client(args.api_url)
        token = os.getenv("HOLY_HYMNS_UPLOAD_TOKEN", "")
        owns_session = False
        try:
            if not token:
                email = args.email or input("Administrator email: ").strip()
                password = getpass.getpass("Administrator password: ")
                session = client.post("/auth/login", {"email": email, "password": password})
                del password
                token = session.get("token", "")
                if not isinstance(token, str) or not token:
                    raise UploadError("Sign-in did not return a session")
                owns_session = True
                if session.get("user", {}).get("role") not in ("admin", "owner"):
                    raise UploadError("An administrator or owner account is required")
            report = upload(client, entries, token, args.dry_run)
            if args.report:
                args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
            print(json.dumps(report, ensure_ascii=False, indent=2))
            return 2 if report["conflicts"] or report["skipped"] else 0
        finally:
            if owns_session:
                try:
                    client.post("/auth/logout", {}, token)
                except (UploadError, ValueError):
                    print("Could not close the upload session. Use Account → Sign out all devices if needed.", file=sys.stderr)
    except (UploadError, OSError, ValueError, EOFError) as error:
        print(f"Upload failed: {error}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("Upload interrupted. Rerun safely to resume.", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
