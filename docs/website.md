# Public lyrics website

The Go API embeds the public website templates, stylesheet, reader script, image and Malayalam font under `backend/internal/server/website/`. Rebuild the API image to publish changes. The public pages read the current **published** database snapshot; publishing and unpublishing take effect without a static export. There are no new migrations or JavaScript framework dependencies.

## Routes and indexing

- `/`: landing page, hero, Browse lyrics anchor, six hymn previews, purpose and reader FAQs.
- `/lyrics`: paginated collection and Malayalam/Manglish search. Pagination has distinct canonical URLs and crawlable next/previous links. Search results are `noindex, follow`.
- `/lyrics/<title-slug>-<uuid>`: full HTML lyrics, unique metadata, canonical URL, MusicComposition and breadcrumb structured data. Old title slugs redirect to the current URL. Unpublished or missing hymns return a noindex 404.
- `/sitemap.xml`: current published hymn URLs and collection pages, with publication timestamps on hymns. `/robots.txt` advertises the sitemap.
- `/site-assets/*`: embedded public assets; fingerprinted asset links have immutable caching. The hero is an optimized 68 KB WebP; the Malayalam font is served locally under its included OFL license.
- `/app/`: existing Expo reader, saved hymns and account/editor features. Caddy serves the previous web export here and sends `X-Robots-Tag: noindex, follow`.
- `/v1/*` and `/auth/*`: existing API and email account flows. Public pages on the backend hostname redirect to the canonical website origin.

The canonical origin is explicitly `https://holyhymns.in`, not a value derived from request headers. Both scripts are present in HTML without JavaScript; reader controls progressively enhance that content. Structured JSON uses Go JSON escaping and HTML uses `html/template`. Draft data and private review notes are never rendered. HTML is revalidated rather than cached across publication changes.

The sitemap is available for submission to Google Search Console or Bing Webmaster Tools. Deployment makes pages crawlable; indexing and ranking are determined by search engines. Search Console submission is a separate account operation and has not been configured by this change.

## Verification

Use the project's pinned Go binary. With a disposable PostgreSQL 17 database:

```sh
cd backend
TEST_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:PORT/DATABASE?sslmode=disable' /Users/grejo.j/.gvm/gos/go1.25.5/bin/go test -trimpath ./...
/Users/grejo.j/.gvm/gos/go1.25.5/bin/go vet ./...
```

Website integration tests cover publication isolation, immediate unpublishing, metadata, redirects, invalid URLs, search, sitemap XML, HTML/JSON escaping and untrusted Host headers. Browser checks cover the hero CTA, search, script tabs with keyboard navigation, text-size limits, pagination and narrow layouts.

## Deployment and rollback

Use `deploy/holy-hymns.caddy` with the host Caddy configuration. Keep the existing Expo files at `/var/www/holy-hymns/current` and their root-relative `/_expo/*` and `/assets/*` URLs. Validate the complete Caddyfile before a graceful reload.

Recreate only the API service with the existing VM environment and a new immutable image, under `/opt/holy-hymns/.deploy.lock`. Retain the previous image, release directory and site configuration. Check `/healthz`, the homepage, a hymn, sitemap, `/app/`, email landing pages and backend API before considering the release healthy. No database restore is required to roll back this website-only change.

The 2 October rollout preserves the newer, independently deployed Sentry backend from commit `e62daed` by building on the combined source at `e98ae8c`, plus these website changes. That initial release source was assembled outside the working checkout. The monitoring changes have since merged into `main`; this website commit is based on that updated branch so future builds include both features. The VM release contains source provenance and hashes for the deployed image/configuration.
