# Go API

Use Go 1.25.5 and PostgreSQL 17. On this workstation, invoke `/Users/grejo.j/.gvm/gos/go1.25.5/bin/go` directly.

Run the self-contained tests with `go test ./...`. For database-backed acceptance and security tests, set `TEST_DATABASE_URL` to a **disposable local PostgreSQL database**, then run `go test -race ./...` and `go vet ./...`. Use the full Go path above on this workstation. The test database account needs schema creation and extension privileges. Integration tests create and remove isolated schemas; tests are skipped when this variable is absent. Never use production credentials for tests.

The tests exercise draft/public separation, optimistic edits, publication and reconnection, Malayalam and mixed-script search, reader authorization, duplicate-safe import, account lifecycle and owner protection. Identity tests verify signed JWT rejection cases, Apple code exchange and revocation against local mocks, SMTP delivery and persistent email quotas.

The executable accepts `serve`, `migrate`, `health`, `import`, `export-lyrics`, `owner` and `recover-owner`. The `export-lyrics` command writes an editable folder without a database. Configuration comes from environment variables listed in the root `.env.example`. Migrations run under a PostgreSQL advisory lock before startup; the Docker migration service also applies them before the API becomes healthy.

See [the API contract](../docs/openapi.json), [deployment](../docs/deployment.md), and [recorded verification](../docs/verification.md). Public changes have a durable database revision. SSE messages are invalidation hints; clients reconcile from the catalogue on each foreground/reconnection. Only explicit publication makes a draft public.

Browsing uses worship categories such as Entrance Songs, Communion Songs, Adoration Songs, Recession Songs, Mother Mary Songs, and Liturgy. Migration `005_worship_categories.sql` seeds these categories and replaces labels identified from the first imported revisions. Clear aliases (for example Marian → Mother Mary Songs) are mapped; unrelated artist, album, and language labels are retained in `songs.source_labels`. Existing editorial categories are preserved. Unclassified songs remain in All Songs and can be categorized in the editor. Draft and published category assignments migrate independently, without publishing draft lyrics.

Future imports retain original labels as metadata and only assign categories through exact aliases in `category_import_labels`. Built-in categories appear in the picker even before songs are assigned; other editorial categories appear to readers once used by a published song. The category selection follows the requested naming and common worship types in [Madely’s song categories](https://madely.us/lyrics/kurbanayennal-enthanamme-onnu-paranju-tharamo/).
