import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import urllib.error

import upload_lyrics as uploader


def fixture_entry():
    return {
        "sourceId": "tag:blogger.com,1999:blog-6936100729546217639.post-123",
        "sourceUrl": "https://grejolyrics.blogspot.com/2023/12/test.html",
        "title": "Test hymn", "titleMalayalam": "പരിശോധന",
        "lyricsMalayalam": "കർത്താവേ കനിയണമേ\n\nസമാധാനം നൽകണമേ",
        "lyricsManglish": "Karthaave kaniyaname\n\nSamaadhaanam nalkaname",
        "credits": "Lyrics: test fixture", "labels": ["Prayer"],
        "links": [{"label": "Song", "url": "https://example.com/song"}],
        "reviewNotes": ["Review before publishing"],
    }


def make_bundle(folder, entry=None):
    entry = entry or fixture_entry()
    song = {key: value for key, value in entry.items() if not key.startswith("lyrics")}
    song.update(sourceHash="a" * 64, contentFingerprint=uploader.fingerprint(entry))
    for key, path_key, directory in (
        ("lyricsMalayalam", "malayalamFile", "malayalam"),
        ("lyricsManglish", "manglishFile", "manglish"),
    ):
        song[path_key] = f"{directory}/test-123.txt" if entry[key] else ""
        if entry[key]:
            (folder / directory).mkdir(exist_ok=True)
            (folder / song[path_key]).write_text(entry[key], encoding="utf-8")
    manifest = {"schemaVersion": 1, "songs": [song], "skipped": []}
    (folder / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False), encoding="utf-8")
    return manifest


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.folder = Path(self.temp.name)

    def test_preserves_unicode_stanzas_and_original_hash(self):
        make_bundle(self.folder)
        entries, edited = uploader.load_bundle(self.folder)
        self.assertEqual(edited, 0)
        self.assertEqual(entries[0]["lyricsMalayalam"], fixture_entry()["lyricsMalayalam"])
        self.assertEqual(entries[0]["hash"], "a" * 64)

    def test_local_lyric_and_metadata_edits_change_hash(self):
        manifest = make_bundle(self.folder)
        lyric = self.folder / manifest["songs"][0]["manglishFile"]
        lyric.write_text("Edited by reviewer", encoding="utf-8")
        entries, edited = uploader.load_bundle(self.folder)
        self.assertEqual(edited, 1)
        self.assertNotEqual(entries[0]["hash"], "a" * 64)
        first_hash = entries[0]["hash"]
        manifest["songs"][0]["credits"] = "Corrected credit"
        (self.folder / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        entries, _ = uploader.load_bundle(self.folder)
        self.assertNotEqual(entries[0]["hash"], first_hash)

    def test_missing_script_is_absent_not_fabricated(self):
        entry = fixture_entry()
        entry["lyricsManglish"] = ""
        make_bundle(self.folder, entry)
        entries, edited = uploader.load_bundle(self.folder)
        self.assertEqual(entries[0]["lyricsManglish"], "")
        self.assertEqual(edited, 0)

    def test_rejects_missing_file_and_duplicate_sources_before_upload(self):
        manifest = make_bundle(self.folder)
        manifest["songs"].append(manifest["songs"][0])
        (self.folder / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        with self.assertRaisesRegex(uploader.UploadError, "duplicate"):
            uploader.load_bundle(self.folder)
        make_bundle(self.folder)
        (self.folder / "malayalam/test-123.txt").unlink()
        with self.assertRaisesRegex(uploader.UploadError, "Missing"):
            uploader.load_bundle(self.folder)

    def test_file_paths_cannot_escape_bundle(self):
        for path in ("../private.txt", "/tmp/private.txt", "manglish/../private.txt"):
            with self.subTest(path=path), self.assertRaises(uploader.UploadError):
                uploader.read_lyrics(self.folder, path, "manglish")
        with tempfile.TemporaryDirectory() as outside:
            secret = Path(outside) / "private.txt"
            secret.write_text("not lyrics")
            (self.folder / "manglish").mkdir()
            (self.folder / "manglish/link.txt").symlink_to(secret)
            with self.assertRaisesRegex(uploader.UploadError, "unsafe"):
                uploader.read_lyrics(self.folder, "manglish/link.txt", "manglish")

    def test_url_validation_matches_server_canonical_source_rules(self):
        for value in (
            "https://GREJOLYRICS.BLOGSPOT.COM/2023/12/test.html",
            "https://grejolyrics.blogspot.com/2023/12/test.html?",
            "https://@grejolyrics.blogspot.com/2023/12/test.html",
        ):
            with self.subTest(value=value), self.assertRaises(uploader.UploadError):
                uploader.public_https_url(value, source=True)
        with self.assertRaises(uploader.UploadError):
            uploader.public_https_url("https://example.com:invalid/song")

    def test_validate_only_needs_no_server_or_credentials(self):
        make_bundle(self.folder)
        with mock.patch.object(uploader, "Client", side_effect=AssertionError("network forbidden")), contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(uploader.main(["--folder", str(self.folder), "--validate-only"]), 0)

    def test_length_prefix_fingerprint_avoids_field_boundary_collision(self):
        first, second = fixture_entry(), fixture_entry()
        first.update(title="ab", titleMalayalam="c")
        second.update(title="a", titleMalayalam="bc")
        self.assertNotEqual(uploader.fingerprint(first), uploader.fingerprint(second))

    def test_fingerprint_matches_go_exporter_vector(self):
        entry = dict(
            sourceId="tag:blogger.com,1999:blog-6936100729546217639.post-42",
            sourceUrl="https://grejolyrics.blogspot.com/2020/01/karthave.html",
            title="Karthave", titleMalayalam="കർത്താവേ",
            lyricsMalayalam="കർത്താവേ\u200d\n\nനാഥാ (2)", lyricsManglish="Karthave\n\nNadha (2)",
            credits="Lyrics: A", labels=["Mass", "കൃപ"],
            links=[dict(label="Watch", url="https://youtu.be/example")],
            reviewNotes=["Review words", "Check credits"],
        )
        self.assertEqual(uploader.fingerprint(entry), "fdaafbcf1e20d1866f2763db9b640efef1a51fdbc7551a20c062d6f846aaa6cd")


class ClientTests(unittest.TestCase):
    def test_credentials_require_https_except_loopback(self):
        for url in ("http://example.com", "https://user:secret@example.com", "https://example.com/?token=secret", "file:///tmp/file", "http://127.0.0.1.example.com"):
            with self.subTest(url=url), self.assertRaises(uploader.UploadError):
                uploader.api_base(url)
        self.assertEqual(uploader.api_base("https://hymns.example.com/"), "https://hymns.example.com/v1")
        self.assertEqual(uploader.api_base("http://127.0.0.1:8080/v1"), "http://127.0.0.1:8080/v1")

    def test_redirects_never_forward_credentials(self):
        self.assertIsNone(uploader.NoRedirect().redirect_request(None, None, 302, "Found", {}, "https://other.example"))

    def test_malformed_token_never_enters_a_header_or_error_message(self):
        client = uploader.Client("https://hymns.example.com")
        with mock.patch.object(client.opener, "open", side_effect=AssertionError("network forbidden")):
            with self.assertRaisesRegex(uploader.UploadError, "Invalid session token format") as error:
                client.post("/admin/imports/lyrics", {}, "private-token\nother-header")
        self.assertNotIn("private-token", str(error.exception))

    def test_retry_is_bounded_and_auth_is_not_retried(self):
        client = uploader.Client("https://hymns.example.com")
        with mock.patch.object(client.opener, "open", side_effect=urllib.error.URLError("secret must not be logged")) as request, mock.patch.object(uploader.time, "sleep"):
            with self.assertRaisesRegex(uploader.UploadError, "Connection failed") as result:
                client.post("/admin/imports/lyrics", {}, "private-token", retry=True)
            self.assertEqual(request.call_count, 3)
            self.assertNotIn("secret", str(result.exception))
            self.assertNotIn("private-token", str(result.exception))
            request.reset_mock()
            with self.assertRaises(uploader.UploadError):
                client.post("/auth/login", {}, retry=False)
            self.assertEqual(request.call_count, 1)

    def test_upload_keeps_dry_run_and_reports_conflicts(self):
        client = mock.Mock()
        client.post.return_value = dict(total=1, created=0, unchanged=0, conflicts=1, skipped=0, review=["existing song preserved"])
        with contextlib.redirect_stderr(io.StringIO()):
            report = uploader.upload(client, [fixture_entry()], "token", dry_run=True)
        self.assertEqual(report["conflicts"], 1)
        self.assertTrue(client.post.call_args.args[1]["dryRun"])
        self.assertEqual(report["issues"][0]["sourceId"], fixture_entry()["sourceId"])

    def test_login_session_is_closed_after_failed_upload(self):
        with tempfile.TemporaryDirectory() as folder:
            make_bundle(Path(folder))
            client = mock.Mock()
            client.post.side_effect = [{"token": "private-token", "user": {"role": "owner"}}, uploader.UploadError("network failure"), {}]
            with mock.patch.dict(uploader.os.environ, {}, clear=True), mock.patch.object(uploader, "Client", return_value=client), mock.patch.object(uploader.getpass, "getpass", return_value="private-password"), contextlib.redirect_stderr(io.StringIO()) as output:
                result = uploader.main(["--folder", folder, "--api-url", "https://hymns.example.com", "--email", "owner@example.com"])
            self.assertEqual(result, 1)
            self.assertEqual(client.post.call_args.args[0], "/auth/logout")
            self.assertNotIn("private-password", output.getvalue())
            self.assertNotIn("private-token", output.getvalue())


if __name__ == "__main__":
    unittest.main()
