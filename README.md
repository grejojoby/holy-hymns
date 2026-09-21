# Holy Hymns

An online-first Malayalam Christian lyric library for iOS and Android, with a self-hosted Go/PostgreSQL backend and an in-app editorial workspace. Readers browse without an account; only authenticated admins see management controls. Published changes refresh connected apps through Server-Sent Events.

The software uses open-source components and has no mandatory backend subscription. VM, domain, SMTP, object-storage usage and store memberships remain your responsibility; the application does not provision or purchase cloud resources. Google/Apple sign-in needs your developer configuration.

## Local setup

Requirements: Docker with Compose v2; the configured Go 1.25.5 installation; Node supported by the mobile Expo version; Xcode for native iOS and Android Studio for native Android. Docker is optional when using an existing local PostgreSQL 17 installation.

```sh
cp .env.example .env
docker compose -f compose.yaml -f compose.dev.yaml up --build -d
```

The API binds to `127.0.0.1:8080`; the development override exposes PostgreSQL only on `127.0.0.1:5432`. Migrations run before the API. The sample password is explicitly local-only; replace it before deploying. Blank email/provider settings leave those integrations unconfigured.

Create your first owner using a password supplied through the environment, never a command-line argument:

```sh
read -r -s HOLY_HYMNS_OWNER_PASSWORD
export HOLY_HYMNS_OWNER_PASSWORD
docker compose run --rm -e HOLY_HYMNS_OWNER_PASSWORD api owner --email your-email@example.com --name 'Your name'
unset HOLY_HYMNS_OWNER_PASSWORD
```

Start the mobile app after configuring `mobile/.env` from its example. Set `EXPO_PUBLIC_API_URL=http://localhost:8080/v1` for the iOS simulator, `http://10.0.2.2:8080/v1` for the Android emulator, or your development machine's LAN address ending in `/v1` for a physical device. For a physical device, set local-only `API_BIND=0.0.0.0` and restrict your machine's firewall to your LAN.

```sh
cd mobile
npm ci
npm run ios
# Alternatively: npm run android, or npm run web for a browser preview.
```

Native social sign-in requires a development/native build. A browser preview is useful for visual review and email sign-in but does not substitute for device testing. See [deployment](docs/deployment.md) for production configuration and [App Review](docs/app-review.md) for release checks.

## CI/CD to the Oracle VM

GitHub Actions tests the backend, mobile app and deployment tooling, then publishes API and backup images for **ARM64 (Oracle Ampere)** and **AMD64**. Once VM secrets are configured and `DEPLOY_ENABLED=true`, a successful `main` build deploys immutable images over SSH using Docker Compose. Builds run on GitHub; the VM only pulls and runs images.

Follow [the CI/CD setup](docs/ci-cd.md) for VM preparation, repository secrets, free usage limits and recovery. Production `.env` stays on the VM. Automatic deployment remains disabled until configured. The [Oracle VM deployment record](docs/oracle-vm.md) documents the current manually deployed backend.

## Import the initial collection

The editable extraction is in [`content/lyrics`](content/lyrics/README.md), with separate Malayalam and Manglish text files, source references and a review folder for material that cannot be imported as one supported-script hymn. To upload the extracted files after deploying the server, run from this project directory:

```sh
python3 scripts/upload_lyrics.py --validate-only
python3 scripts/upload_lyrics.py --api-url https://YOUR_API_DOMAIN --dry-run
python3 scripts/upload_lyrics.py --api-url https://YOUR_API_DOMAIN
```

The uploader asks for your administrator email/password, creates drafts only, and closes its own login session afterward. It needs Python 3.10+ with no extra packages. Repeat runs use durable Blogger source IDs and preserve songs already edited in the app. See the [folder instructions](content/lyrics/README.md) for review, selective uploads and extraction commands.

Only the owner's [Grejo Lyrics](https://grejolyrics.blogspot.com/) public Atom feed is supported for direct network imports. The importer also reads Blogger Atom/XML exports. It preserves source IDs, available scripts, stanza breaks, labels, credits and external links; it never generates translations or publishes songs.

```sh
docker compose run --rm api import --url 'https://grejolyrics.blogspot.com/feeds/posts/default?max-results=150' --dry-run
docker compose run --rm api import --url 'https://grejolyrics.blogspot.com/feeds/posts/default?max-results=150'
```

A complete 331-post snapshot is included in `content/grejo-lyrics.atom` for reproducible offline import:

```sh
docker compose run --rm -v "$PWD/content:/import:ro" api import --file /import/grejo-lyrics.atom --dry-run
docker compose run --rm -v "$PWD/content:/import:ro" api import --file /import/grejo-lyrics.atom
```

For another Blogger export, mount its containing folder read-only and use `import --file /import/blog.xml`. Review all imported drafts in the app, especially mixed-script text, partial summaries, repeated titles and media-only posts. Exclude entries with no lyrics. Reimporting a source does not overwrite subsequent editorial changes.

The live public feed reported **331 posts on 19 September 2026**. That is a source-post count, not a count of approved songs. The app starts with no publicly published collection until an administrator approves content.

## Verification

```sh
cd backend
/Users/grejo.j/.gvm/gos/go1.25.5/bin/go test ./...
/Users/grejo.j/.gvm/gos/go1.25.5/bin/go vet ./...
```

```sh
cd mobile
npm run typecheck
npm test
npm run export:native
```

```sh
python3 -m unittest discover -s scripts -p 'test_*.py'
docker compose -f compose.yaml -f compose.dev.yaml config --quiet
```

Run database-backed tests against a disposable PostgreSQL database using the test instructions in `backend`; never point tests at the production catalogue. Native builds, production social-provider validation, HTTPS deployment and an actual encrypted backup/restore drill require their respective local tools and credentials.

See [the verification record](docs/verification.md) for the bounded local load check and repeatable encrypted restore fixture.

## Layout

- `backend`: REST API, authentication, catalogue, migrations and Blogger import.
- `mobile`: React Native/Expo app, reader experience and admin screens.
- `compose.yaml`, `deploy`, `scripts`: VM packaging, HTTPS and bounded encrypted backups.
- `docs`: API contract, deployment instructions and store review checklist.
