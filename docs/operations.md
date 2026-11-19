# Operations

## Running without Docker

Requirements: Go 1.25.3, PostgreSQL (version 16 was used for the pilot),
and the `migrate` CLI from golang-migrate. Run commands from the repository root.

Create a dedicated database and set a connection string for your installation.
This example assumes an existing local PostgreSQL role named `ratingdon`:

```sh
createdb -h localhost -U ratingdon ratingdon
export DATABASE_URL='postgres://ratingdon@localhost:5432/ratingdon?sslmode=disable'
migrate -path migrations -database "$DATABASE_URL" up
go run ./cmd/feedback
```

If your server requires a password, configure authentication for your PostgreSQL
installation separately. The Go application does not create databases or apply
migrations. The default address is `127.0.0.1:8080`. Open `/admin` and
`/tablet?device_id=reception-1`. Tokens are optional for local loopback access.
Press Ctrl+C in the application terminal to stop the server.

## Configuration

The application reads environment variables at startup. `.env.example` is a
reference; Go does not load `.env` automatically. Compose has its own environment
variable interpolation mechanism.

| Variable | Purpose / default |
| --- | --- |
| `APP_ADDR` | HTTP listen address, `127.0.0.1:8080` |
| `DATABASE_URL` | pgx connection string. Current fallback: `host=/tmp port=5432 user=sh dbname=ratingdon`; set explicitly for your installation |
| `DEVICES_JSON` | Device array; default: `[{"id":"reception-1","name":"Ресепшен · планшет 1"}]` |
| `REQUEST_TTL` | Request lifetime, `30s`; range `5s`–`10m` |
| `ADMIN_TOKEN` | Receptionist token |
| `DEVICE_TOKENS_JSON` | Object such as `{"reception-1":"token"}` |
| `SLIDESHOW_URL` | Full HTTP(S) URL of a supported player; empty disables the proxy |
| `POSTGRES_PASSWORD` | Password for the Compose database; not read directly by Go |
| `TEST_DATABASE_URL` | Integration test database; not an application setting |

Device IDs must be unique and contain 1–64 ASCII letters, digits, `_`, or `-`.
Names must be nonempty. A non-loopback `APP_ADDR` requires a receptionist token
and a token for every tablet, each at least 24 characters long. Tokens must
be distinct and contain no whitespace or control characters.

## Docker Compose

First-run commands are in the [README](../README.md#getting-started).
Compose starts PostgreSQL 16, a migration container, and the Go application.
The application starts only after the database is healthy and migrations succeed.
Data is stored in a named volume. PostgreSQL has no published host port.

```sh
docker compose ps
docker compose logs --tail=100 app
docker compose logs --tail=100 migrate
```

To rebuild after code changes, using your existing environment values:

```sh
docker compose up --build -d --wait
```

Keep the original PostgreSQL password for an existing volume. Back up the database
before schema updates. `docker compose down` preserves the volume;
`docker compose down -v` deletes its data.

## Migrating an existing database

Before changing the schema, stop the application, create a backup, and verify
restoration in a separate database. Keep backups out of Git.
The following assumes a URL accepted by both PostgreSQL CLI tools and migrate:

```sh
pg_dump --dbname="$DATABASE_URL" --format=custom --file=/tmp/ratingdon-before-migration.dump
migrate -path migrations -database "$DATABASE_URL" version
migrate -path migrations -database "$DATABASE_URL" up
migrate -path migrations -database "$DATABASE_URL" version
```

The example backup path is overwritten on subsequent runs. Use unique filenames
and dedicated protected storage for regular backups.
The current schema version is 4, with `dirty=false`.

| Version | Change |
| --- | --- |
| 1 | `ratings` table |
| 2 | `rating_requests` table |
| 3 | Request statuses, lifetime, constraints, and indexes |
| 4 | Persistent tablet registry with token digests |

Migration 3 preserves existing ratings. Legacy requests without reliable lifetime
information are closed: requests with ratings become answered, and the others
become expired. Compare rating counts before and after the update.

If `dirty=true`, inspect which SQL statements were executed before proceeding.
`migrate force` changes the version marker but does not repair the schema.
Review rollback migrations before running them: they may remove tables or columns.

## Slideshow integration

```sh
export SLIDESHOW_URL='http://slideshow-host:5000/slideshow/reception'
go run ./cmd/feedback
```

Replace this example address with your compatible player URL.
For Compose, the source must be reachable from the container. Container-local
`localhost` does not refer to the host machine or a different server.

The adapter targets these transmitter routes: `/slideshow/<slug>`, `/api/time`,
`/api/slideshow/slug/<slug>/status`, and `/uploads/<filename>`.
The application's proxy entry point is `/slideshow/`.
Content management remains in the external system.

## Network access for tablets

The default Compose configuration publishes HTTP only on `127.0.0.1`.
For reception deployment, configure HTTPS through a reverse proxy at an address
reachable by tablets. Forward the correct Host matching the browser URL;
the API checks Origin. For native execution, choose a network `APP_ADDR` and
configure all required tokens. Do not transmit Bearer tokens over public HTTP.

Tokens are shared secrets for the receptionist role and individual devices,
not user accounts. Rotation requires updating the environment, restarting
the application, and replacing the tokens in the interfaces.

## Tests and troubleshooting

```sh
go test ./...
go vet ./...
go test -race ./...
```

Integration tests create and remove unique PostgreSQL schemas. Use a separate
test database and a role allowed to create schemas:

```sh
export TEST_DATABASE_URL='postgres://ratingdon@localhost:5432/ratingdon_test?sslmode=disable'
go test -race -count=1 ./...
```

Without this variable, PostgreSQL storage tests are skipped. Other tests cover
validation, HTTP handlers, configuration, and the slideshow proxy.

| Symptom | Check |
| --- | --- |
| Application exits immediately | Environment variables, database connectivity, port availability |
| `/ready` returns 503 | PostgreSQL connection |
| Request creation returns 409 `device_offline` | Correct tablet page is open and polling `/state` |
| Request creation returns 409 `active_request` | Previous request is still active |
| 401 | Correct role or device token |
| 403 | Browser Origin and forwarded Host |
| Player fails to load | `SLIDESHOW_URL`, reachability from the process/container, supported routes |

The main application logger writes JSON to stderr. The `/health` and `/version`
handlers additionally print the request method to stdout; logging is not yet
fully uniform. SIGINT/SIGTERM allow up to ten seconds for HTTP requests to finish.
The startup database connection check has a five-second timeout.
Device heartbeat state is restored by polling after a restart.

## Tablet enrollment

After applying migration 4, add a tablet by name in `/admin` and open the returned
link on that device. No server restart or `DEVICES_JSON` change is required.
The browser persists its key in local storage. Configured devices continue to
work with `DEVICE_TOKENS_JSON`. Enrolled devices always require their own key,
including on loopback. Keep enrollment links private and backed up securely.
There is currently no UI for revoking or renaming enrolled devices.

For reliable unattended operation, configure the tablet OS/browser in kiosk mode
and prevent screen sleep in device settings. Web fullscreen and wake lock are
best-effort conveniences and do not replace kiosk configuration.
