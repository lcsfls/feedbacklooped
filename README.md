# feedbacklooped

A minimal, self-hosted, white-label feedback portal with a kanban board. Let users report bugs and ideas anonymously, with screenshots and screen recordings, and close the loop with them through a private status link.

- **Anonymous:** no account, no email address. Submitters get a private status link.
- **Screenshots & videos:** drag & drop, paste with <kbd>Ctrl</kbd>+<kbd>V</kbd>, screen recording right in the browser.
- **Board:** Inbox → In review → Planned → Done / Declined, with drag & drop on the board and on each card’s page.
- **Two-way conversation:** your replies show up on the submitter's status page, and they can add details there.
- **Public roadmap** (optional, titles only).
- **Notifications** via ntfy, webhook (Slack/Mattermost/n8n compatible) or email.
- **Multiple admins** with display names, shown next to replies and notes.
- **Spam protection without third parties:** invisible proof-of-work check (ALTCHA-compatible), honeypot, rate limit.
- **GDPR-friendly:** no IP addresses stored, no cookies for visitors, everything self-hosted (fonts included), metadata stripped from images, automatic deletion of closed feedback, self-service deletion via the status link, imprint and privacy policy pages with a template.
- **White-label:** your own logo, wordmark and colors in a few clicks. Light and dark mode follow the system.
- **One binary, zero dependencies:** Go standard library only. Data is a JSON file plus an uploads folder.

## Run with Docker

```bash
docker compose up -d --build
```

Then open `https://feedback.your-domain.com/admin` and create the first admin account. **Do this right after the first start:** until an account exists, anyone can open the setup page. Alternatively, preset it with `FL_ADMIN_USER` and `FL_ADMIN_PASSWORD`.

Everything else (name, branding, categories, limits, spam protection, data retention, imprint and privacy policy, roadmap, notifications) is configured in the browser under **Settings**; further admins are added under **Users**.

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

## Spam protection

Public forms are protected in three layers, none of which involves a third party:

1. **Proof of work** (on by default): the browser fetches a signed challenge and searches for a number whose SHA-256 hash matches it, in the background, before the visitor hits send. Each challenge expires after 20 minutes and can be used once. The protocol is compatible with [ALTCHA](https://altcha.org), implemented here with the Go standard library. Difficulty is adjustable; JavaScript is required while it is on.
2. **Honeypot field and minimum fill time** against simple bots.
3. **Rate limit** per sender (default 10 per hour), based on a salted hash of the IP address held in memory only.

## Data protection (GDPR)

- No IP addresses are written anywhere; the rate-limit key lives in memory for at most an hour.
- No cookies for visitors. Admins get one session cookie (technically necessary).
- Fonts, scripts, icons: all served from your server. Nothing is loaded from CDNs or font services. Optionally use the OS font instead.
- EXIF/XMP/IPTC is stripped from JPEG, PNG and WebP.
- **Retention:** closed cards (Done/Declined) can be deleted automatically after N days, attachments included.
- **Right to erasure:** submitters can delete their feedback, messages and attachments through their status link.
- **Imprint & privacy policy:** link to existing pages or write them in the settings; they are linked in the footer of every page. English and German privacy policy templates describe what the tool does; fill in your details and have them checked.
- Notifications send the feedback text to the channels you configure (ntfy, webhook, email). Mention them in your privacy policy.

## Admins

Any number of admin accounts, all with the same rights. Each has a username, an optional display name shown next to their replies and notes, and their own password. Submitters see “Team” by default, or “Name · Team” if you enable it. Changing a password logs that user out everywhere else; deleting a user ends their sessions immediately. Under **Account**, each admin can add an email address and opt in to notifications about new feedback and replies; those addresses are added to the recipients configured under Settings → Notifications. The notification test can send a test email to any address.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `FL_LISTEN` | `:8080` | Listen address/port |
| `FL_DATA` | `./data` (`/data` in the container) | Data directory |
| `FL_TRUST_PROXY` | `false` | Set to `true` behind a reverse proxy to trust `X-Forwarded-For/-Proto/-Host` |
| `FL_BASE_URL` | – | Public URL, e.g. `https://feedback.example.com` (initial value only, editable in Settings later) |
| `FL_ADMIN_USER` | `admin` | Username for `FL_ADMIN_PASSWORD` |
| `FL_ADMIN_PASSWORD` | – | Creates this admin on start, or resets its password if it differs. Doubles as a password reset. |
| `FL_BIND` | `127.0.0.1:8080` | Published port in `docker-compose.yml` |

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

## Security

- Passwords hashed with PBKDF2-SHA256 (600,000 iterations), constant-time login for unknown usernames.
- HMAC-signed session cookie, `HttpOnly`, `SameSite=Strict`, CSRF protection via Origin/`Sec-Fetch-Site`.
- Login rate limit per sender; proof-of-work, honeypot, minimum fill time and a configurable submission limit on public forms.
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

## License

[GNU Affero General Public License v3.0](LICENSE). If you run a modified version as a service, you must make its source available to its users.
