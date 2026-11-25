# UI guide

[Русский](ui.ru.md)

## Receptionist screen

URL: `/admin`. If authorization is enabled, enter the receptionist token.
Use **«Подключить планшет»** (Connect a tablet) to enter a name and generate
an enrollment link and a QR code. Scan the code with the tablet camera,
or open the link once on the tablet. It contains a device
secret, so share it only with staff. The QR contains the complete enrollment
URL, including the token. It is generated in the browser by a bundled encoder;
no enrollment data is sent to external QR services. If QR generation fails,
the link remains available. Use the application's network address rather than
`localhost` when enrolling a physical tablet. The plaintext key is returned only when
the device is created; the database stores its SHA-256 digest.

The device is saved in PostgreSQL and remains registered after server restarts.
The tablet appears online when its page starts polling. This is enrollment
and automatic presence detection, not a network scan of unrelated devices.
Choose a tablet in **«Планшет для оценки»** (Rating tablet), or show all cards.
Each tablet card shows connectivity, its current request, and its latest answer.
Click **«Попросить оценку»** (Request a rating) when the tablet is online and has
no active request. The server generates the request ID and expiration time.

## Guest screen

URL: `/tablet?device_id=reception-1`.
To provision a token, open
`/tablet?device_id=reception-1#token=TABLET_KEY`.
The fragment is removed from the address. The token is stored in local browser
storage so it survives tab and browser restarts. Clearing site data, private
browsing, or a browser policy can require provisioning again. If persistent
storage is unavailable, the application falls back to tab session storage.
Keep the enrollment link in a secure place; the server cannot recover the key.

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

## Fullscreen and screen sleep

Tap **«На весь экран»** (Fullscreen) on the tablet to make the entire application
fullscreen. The slideshow, form, and thank-you screen remain in that same page;
normal rating transitions do not exit fullscreen. If fullscreen is exited,
the control becomes available again. The application also requests a screen wake
lock when supported, and attempts to reacquire it when the page becomes visible.

A website cannot guarantee that users stay in fullscreen: browsers require a
user gesture and allow leaving it. For unattended tablets, use the operating
system's kiosk or single-app mode. Setup depends on the tablet OS and browser.
Wake lock requires a secure context (HTTPS or a browser-trusted local origin),
is not supported by all devices, and may be denied by device settings.
