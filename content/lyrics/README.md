# Grejo Lyrics collection

Extracted from the owner's [Grejo Lyrics](https://grejolyrics.blogspot.com/) public feed on 19 September 2026. The live feed contained 331 unique posts. This folder contains unpublished candidates for Holy Hymns; the uploader does not publish them.

| Contents | Count |
| --- | ---: |
| Candidate songs | 288 |
| Malayalam text files | 288 |
| Manglish text files | 288 |
| Songs with both scripts | 288 |
| Posts excluded from automatic upload | 43 |

Source posts originally contained 46 Malayalam and 279 Manglish lyric files (37 with both). Missing counterparts were filled with phonetic transliteration (`scripts/fill_missing_scripts.py`: Manglish→Malayalam via Google Input Tools, Malayalam→Manglish via [ml2en](https://github.com/knadh/ml2en)). Generated text is draft quality—review spelling and wording before publishing. Latin text also still needs editorial review to confirm spelling and remove any remaining prose. Chord markers were removed only where they could be identified without removing lyric words.

## Folder contents

- `malayalam/`: UTF-8 lyric files, preserving Malayalam characters and stanza breaks.
- `manglish/`: UTF-8 romanized lyric files.
- `manifest.json`: upload list, titles, source IDs/URLs, credits, labels, external links, review notes and file references.
- `index.csv`: a browsable index of all 331 posts, including excluded entries and review reasons.
- `summary.json`: extraction counts.
- `review/`: 41 text files needing separate handling: 27 chord arrangements, 10 English/Hindi posts, two multi-song collections, one index page and one image-based post. The image-based entry contains available surrounding text, not an image transcription. The other two excluded posts are download/media-only entries with their sources preserved.
- `source/`: original post HTML for all 331 posts, retained as reference. These files are not uploaded or rendered in the app. View them as text when reviewing markup; embedded media has not been downloaded.

File names combine the title with its Blogger post ID, so different posts with the same title remain distinct. The same filename in each lyric folder belongs to the same song.

## Upload when the server is ready

Use Python 3.10 or newer; there are no packages to install. Run these commands from the Holy Hymns project root. First deploy the current backend, including `POST /v1/admin/imports/lyrics`, and create an owner/admin account using the normal documented setup.

Validate every local file without contacting a server:

```sh
python3 scripts/upload_lyrics.py --validate-only
```

Check the collection against your server without importing:

```sh
python3 scripts/upload_lyrics.py --api-url https://YOUR_API_DOMAIN --dry-run
```

Upload all candidates as drafts:

```sh
python3 scripts/upload_lyrics.py --api-url https://YOUR_API_DOMAIN
```

Replace `https://YOUR_API_DOMAIN` with your real API address. An address already ending in `/v1` also works. The script prompts for your administrator email and password; the password is hidden and is never stored in the folder. It closes the login session it created when finished. For local testing only, an HTTP loopback URL such as `http://127.0.0.1:8080` is allowed.

For an existing Holy Hymns session, `HOLY_HYMNS_UPLOAD_TOKEN` can supply the token without logging in again. This is the app's session token, not a Google/Apple ID token. The script does not revoke externally supplied sessions. Keep credentials out of source control.

Optional arguments:

```sh
# Save the result report outside the collection.
python3 scripts/upload_lyrics.py --api-url https://YOUR_API_DOMAIN --report /tmp/lyrics-upload.json

# Upload one selected source; repeat --source-id to select more.
python3 scripts/upload_lyrics.py --api-url https://YOUR_API_DOMAIN \
  --source-id 'tag:blogger.com,1999:blog-6936100729546217639.post-8331476033853071764'

# Use another extracted folder.
python3 scripts/upload_lyrics.py --folder /path/to/lyrics --validate-only
```

The script validates the whole folder before signing in, sends one bounded request at a time, refuses credential-bearing redirects, and retries temporary import failures at most three times. An interrupted upload can be rerun: source IDs are stored on the server, so progress does not depend on a local state file.

Results distinguish `created`, `unchanged`, `conflicts` and `skipped`. Exit code 0 means success, 1 means an error, and 2 means the run completed with conflicts or skipped records requiring review. In a dry run, `created` means “would create.”

## Review and editing

Before the first upload, you may edit the lyric `.txt` files and the titles, credits, labels, links or review notes in `manifest.json`. Keep source IDs, source URLs, hashes, fingerprints and file references intact. The uploader detects content edits automatically. It ignores the `skipped` list, `review/` and `source/`; those need manual handling, such as separating a collection into individual songs or transcribing an image after review.

Repeat uploads never overwrite an existing song, including app edits. The same source hash is counted as unchanged; changed local content is reported as a conflict. Correct an existing draft in **Account → Manage Holy Hymns**. Do not modify hashes to force an overwrite. Songs imported by the earlier Atom command are also recognized by source ID and preserved, even if a newer parser would extract them differently.

After upload, review drafts, resolve duplicate-title warnings, check lyrics/credits/links (especially generated counterparts), then publish approved songs from the admin screen.

## Repeat extraction

Extraction needs Go but no database. Use a **new output directory**: the exporter refuses to replace a nonempty directory so it cannot erase your lyric edits.

```sh
cd backend
/Users/grejo.j/.gvm/gos/go1.25.5/bin/go run ./cmd/holyhymns export-lyrics \
  --url 'https://grejolyrics.blogspot.com/feeds/posts/default?max-results=150' \
  --out ../content/lyrics-new
```

For a reproducible offline extraction from the saved feed, replace `--url ...` with `--file ../content/grejo-lyrics.atom`. The original snapshot and every source post remain available for comparison.
