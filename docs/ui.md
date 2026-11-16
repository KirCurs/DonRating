# UI guide

## Receptionist screen

URL: `/admin`. If authorization is enabled, enter the receptionist token.
Each tablet card shows connectivity, its current request, and its latest answer.
Click **«Попросить оценку»** (Request a rating) when the tablet is online and has
no active request. The server generates the request ID and expiration time.

## Guest screen

URL: `/tablet?device_id=reception-1`.
To provision a token, open
`/tablet?device_id=reception-1#token=TABLET_KEY`.
The fragment is removed from the address; the token is stored in the tab session.
After that session ends, provisioning must be repeated.

While idle, the tablet shows the slideshow or a local standby screen.
An invitation displays a rating form with scores from 1 to 5.
A successful submission shows a thank-you message, then resumes the slideshow
after two seconds. Unanswered requests close at their deadline.
Temporary submission failures preserve the selected score for retry until expiry.

For local verification, use two tabs: receptionist and tablet.
A physical tablet requires network access to the Go application, its own token,
and HTTPS. The default Compose configuration publishes only on loopback.

## Frontend changes

Files are located in `internal/httpapi/web`:

- `admin.html`, `admin.js`: receptionist interface.
- `tablet.html`, `tablet.js`: guest interface.
- `app.css`, `app.js`: shared styles and API utilities.

Files are embedded in the binary. After changes, restart `go run ./cmd/feedback`
or rebuild the Docker image. The main interface uses local assets;
the external player may have its own CDN dependencies.

The application UI is currently in Russian; the labels quoted here match
what users see on screen.
