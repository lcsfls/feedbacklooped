# feedbacklooped

A minimal, self-hosted, white-label feedback portal with a kanban board. Let users report bugs and ideas anonymously, with screenshots and screen recordings, and close the loop with them through a private status link.

- **Anonymous:** no account, no email address. Submitters get a private status link.
- **Screenshots & videos:** drag & drop, paste with <kbd>Ctrl</kbd>+<kbd>V</kbd>, screen recording right in the browser.
- **Board:** Inbox → In review → Planned → Done / Declined, with drag & drop.
- **Two-way conversation:** your replies show up on the submitter's status page, and they can add details there.
- **Public roadmap** (optional, titles only).
- **Notifications** via ntfy, webhook (Slack/Mattermost/n8n compatible) or email.
- **White-label:** your own logo, wordmark and colors in a few clicks. Light and dark mode follow the system.
- **One binary, zero dependencies:** Go standard library only. Data is a JSON file plus an uploads folder.

## Run with Docker

```bash
docker compose up -d --build
```

Then open `https://feedback.your-domain.com/admin` and set the admin password. **Do this right after the first start:** until a password is set, anyone can open the setup page. Alternatively, preset the password with `FL_ADMIN_PASSWORD`.

Everything else (name, branding, categories, limits, roadmap, notifications) is configured in the browser under **Settings**.

## White-label

Out of the box the portal uses a neutral look and a built-in mark. Under **Settings → Branding** and **Settings → Logo** you can replace that:

| Setting | Effect |
|---|---|
| Wordmark + tag | Name in the header, e.g. `acme` + `support`. The tag is optional and set in mono. |
| Logo | PNG, JPEG, WebP or SVG (max. 2 MB). Replaces the built-in mark and is used as favicon. |
| Hide wordmark text | For logos that already contain the name. |
| Primary color (light / dark) | Buttons, links, focus ring, progress. Dark mode is derived automatically if left empty; text on buttons switches between white and dark for contrast. |
| Mark colors | Recolor the built-in mark instead of replacing it. |
| Hide “Powered by” | Removes the footer credit. |

Unchecked colors and an empty logo fall back to the defaults. **Reset branding** restores them all at once. Branding is stored with the other settings in `feedback.json`, the logo in `branding/` next to it, so a backup of the data directory covers it.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `FL_LISTEN` | `:8080` | Listen address/port |
| `FL_DATA` | `./data` (`/data` in the container) | Data directory |
| `FL_TRUST_PROXY` | `false` | Set to `true` behind a reverse proxy to trust `X-Forwarded-For/-Proto/-Host` |
| `FL_BASE_URL` | – | Public URL, e.g. `https://feedback.example.com` (initial value only, editable in Settings later) |
| `FL_ADMIN_PASSWORD` | – | Sets the admin password on start if it differs. Doubles as a password reset. |

## Reverse proxy

Example for nginx:

```nginx
server {
    server_name feedback.example.com;
    client_max_body_size 600m;          # >= max. files × max. MB from Settings
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_request_buffering off;
    }
    access_log off;                     # if "anonymous" should hold at the proxy too
}
```

Caddy:

```caddy
feedback.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy has no upload size limit by default and writes no access logs unless you add a `log` directive. If Caddy runs in a container, start feedbacklooped with `FL_BIND=172.17.0.1:8085` and proxy to `172.17.0.1:8085`.

With Nginx Proxy Manager, put `client_max_body_size 600m;` under *Advanced*.

## Privacy

- **No IP addresses are stored.** Rate limiting uses a salted hash kept in memory only.
- **EXIF/XMP/IPTC** (GPS, camera, comments) is stripped from JPEG, PNG and WebP without re-encoding. Videos are stored as-is: browser recordings contain no location data, phone videos may.
- Status links are stored as SHA-256 hashes only.
- Attachments are only served to the logged-in admin or with the matching status link.
- Contact details are optional.

Your reverse proxy's access logs are outside this tool. If you promise anonymity, disable or shorten them for this host.

## Security

- Admin password hashed with PBKDF2-SHA256 (600,000 iterations). Changing it ends all sessions.
- HMAC-signed session cookie, `HttpOnly`, `SameSite=Strict`, CSRF protection via Origin/`Sec-Fetch-Site`.
- Login rate limit, honeypot and timestamp against spam bots, max. 10 submissions per hour per sender.
- Uploads are detected by content, not extension. Allowed: PNG, JPEG, GIF, WebP, MP4, MOV, WebM and TXT/LOG. They are served with `nosniff` and a sandbox CSP.
- Strict Content Security Policy without inline scripts. The container runs as non-root (distroless).

## Backup

Back up the volume or data directory: `feedback.json` and `uploads/`. A JSON export of all cards is also available under *Settings → Data*.

## Local development

```bash
go run .
# http://localhost:8080
```

Requires Go 1.24 or newer. No external modules.

## Third-party assets

- Inter Tight and IBM Plex Mono, SIL Open Font License 1.1 (see `web/static/fonts/`). Subsetted to Latin and Latin Extended.
