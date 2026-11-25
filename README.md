# RatingDon

## What it solves

RatingDon collects guest feedback at the reception desk of Don Resort.
It uses tablets that normally display a slideshow: a receptionist requests
feedback, a rating form appears on the selected tablet, and the guest submits
a score from 1 to 5. The tablet returns to the slideshow after submission
or when the request expires.

The server generates request IDs, stores ratings in PostgreSQL, and prevents
duplicate submissions. Receptionists can see tablet connectivity, request
status, and the latest response.

Built with Go, `net/http`, and pgx/v5. The HTML/CSS/JavaScript interface is
embedded in the binary; no separate frontend server is required.
See [Architecture](docs/architecture.en.md) and [HTTP API](docs/api.en.md) for details.

На русском доступны [архитектура](docs/architecture.ru.md), [HTTP API](docs/api.ru.md),
[запуск и эксплуатация](docs/operations.ru.md) и [интерфейс](docs/ui.ru.md).

## Getting started

Requirements: Git, Docker with Docker Compose, and OpenSSL. For the first run:

```sh
git clone https://github.com/KirCurs/DonRating.git
cd DonRating

export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
export ADMIN_TOKEN="$(openssl rand -hex 24)"
export DEVICE_TOKENS_JSON="$(printf '{"reception-1":"%s"}' "$(openssl rand -hex 24)")"
docker compose up --build -d --wait
```

Compose starts PostgreSQL 16 in a separate volume, applies migrations,
and starts the application.

1. Open the [receptionist interface](http://127.0.0.1:8080/admin) and enter `ADMIN_TOKEN`.
2. Open `http://127.0.0.1:8080/tablet?device_id=reception-1#token=TABLET_KEY`,
   replacing `TABLET_KEY` with the `reception-1` value from `DEVICE_TOKENS_JSON`.
3. Wait for the tablet to appear online in the receptionist interface.

Save the database password and tokens in a password manager. Export the same
values on subsequent runs: changing `POSTGRES_PASSWORD` does not change
the password of an existing database volume.

View application logs or stop the stack:

```sh
docker compose logs --tail=50 app
docker compose down
```

`down` preserves data; adding `-v` deletes the database volume.
For a native Go 1.25.3 setup, see [Operations](docs/operations.en.md).
Screen setup is covered in the [UI guide](docs/ui.en.md). Add further tablets
by name in the receptionist interface and open each generated link on its device.
Registration is persistent; no configuration edit or restart is needed.

## Configuration

Set environment variables before starting the application.
[.env.example](.env.example) lists the available settings.
The Go application does not load `.env` files automatically.

| Variable | Purpose |
| --- | --- |
| `APP_ADDR` | Go HTTP address: `127.0.0.1:8080` for native execution; `0.0.0.0:8080` inside Compose |
| `DATABASE_URL` | PostgreSQL connection string. Compose constructs it automatically; set it explicitly for a native setup |
| `POSTGRES_PASSWORD` | PostgreSQL password for Compose |
| `ADMIN_TOKEN` | Receptionist access token |
| `DEVICES_JSON` | Tablet list, e.g. `[{"id":"reception-1","name":"Reception tablet 1"}]` |
| `DEVICE_TOKENS_JSON` | Per-tablet tokens: `{"reception-1":"token"}` |
| `REQUEST_TTL` | Response window: `30s` by default; allowed range `5s`–`10m` |
| `SLIDESHOW_URL` | HTTP(S) URL of a compatible external player. If unset, the tablet displays a local standby screen |

Network binding requires a distinct receptionist token and a token for each
tablet, all at least 24 characters long. Compose requires these tokens and
publishes the application only on `127.0.0.1:8080`. Physical tablets require
network access and HTTPS.

See [Operations](docs/operations.en.md) for player integration, device configuration,
migrations, and troubleshooting.

## Limitations

- Intended for a pilot deployment. HTTP API and UI flows have been tested locally;
  image builds and the full Compose stack still need verification.
- Supports a single application process. Tablet presence is kept in memory
  and restored by polling after a restart.
- Each tablet can have only one active request. Tablets poll the API roughly
  once per second.
- The slideshow proxy targets the existing transmitter's routes;
  other players may require a different adapter.
- No Logus integration, guest or individual service association, user accounts,
  or analytics reports.
- Browser fullscreen cannot prevent exiting the browser. Unattended tablets
  require OS/browser kiosk configuration; screen wake lock is best effort.
- No UI for revoking or renaming enrolled tablets yet.
- Changes to embedded frontend files require rebuilding the application or image.

The logo and fonts come from [Don Resort](https://www.donresort.ru/)
materials for the internal pilot.
