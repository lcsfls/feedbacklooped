# feedbacklooped

A minimal, self-hosted, white-label feedback portal with a kanban board. Let users report bugs and ideas anonymously, with screenshots and screen recordings, and close the loop with them through a private status link.

- **Anonymous:** no account, no email address. Submitters get a private status link.
- **Screenshots & videos:** drag & drop, paste with <kbd>Ctrl</kbd>+<kbd>V</kbd>, screen recording right in the browser.
- **Board:** Inbox → In review → Planned → Done / Declined, with drag & drop on the board and on each card’s page.
- **Two-way conversation:** your replies show up on the submitter's status page, and they can add details there.
- **Public roadmap** (optional, titles only).
- **Private boards** next to the public one, for internal work that never leaves the admin area.
- **Link previews and SEO:** Open Graph / Twitter card with your own image, optional indexing of the public pages with sitemap.
- **Notifications** via ntfy, webhook (Slack/Mattermost/n8n compatible) or email.
- **Multiple admins** with display names, shown next to replies and notes.
- **Spam protection without third parties:** invisible proof-of-work check (ALTCHA-compatible), honeypot, rate limit.
- **GDPR-friendly:** no IP addresses stored, no cookies for visitors, everything self-hosted (fonts included), metadata stripped from images, automatic deletion of closed feedback, self-service deletion via the status link, imprint and privacy policy pages with a template.
- **White-label:** your own logo, wordmark and colors in a few clicks. Light and dark mode follow the system.
- **One binary, zero dependencies:** Go standard library only. Data is a JSON file plus an uploads folder.

> [!WARNING]
> **100 % vibe-coded.** Every line of this project – code, templates, styles, docs – was written by [Claude](https://claude.ai) (Anthropic’s AI model) in a chat with its maintainer, who described what they wanted and tested the result. It has been tested end to end and reviewed for common security issues, but **it has not been audited by an independent human security expert.** Read the [security notes](#security) before you put it on the internet, and treat it like any young, small project: run it behind a reverse proxy, keep it updated, keep backups.

## Run with Docker

```bash
docker compose up -d --build
```

Then open `https://feedback.your-domain.com/admin` and create the first admin account. You need the **setup code** that is printed in the log on the first start (`docker logs feedbacklooped`), so nobody else can claim your instance. Alternatively, preset the account with `FL_ADMIN_USER` and `FL_ADMIN_PASSWORD`; then no setup page is shown.

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

## Boards

The **public board** receives every submission from the feedback form and is the only source for the roadmap. Under **+ Board** you can add any number of **private boards** (e.g. *Internal*, *Sprint*): only admins see them, nothing from outside lands there and they never appear on the roadmap. Switch boards with the tabs above the board; move a card to another board on its page. A submission moved to a private board keeps its status link working.

## Search engines & link previews

Every page carries Open Graph and Twitter card tags, so links shared in Slack, Teams, LinkedIn, WhatsApp or X get a proper preview. Upload your own preview image (PNG/JPEG, 1200 × 630) under **Settings → Link preview image**; a neutral default is used otherwise.

Indexing is **off** by default (`noindex` everywhere, `robots.txt` disallows all). Turn on **Let search engines index the public pages** to allow the form, the roadmap and your own imprint and privacy pages, with canonical links and a `sitemap.xml`. Status pages, attachments, APIs and the admin area are always `noindex` and disallowed. Status pages also use a generic title and preview, so pasting a status link into a chat never reveals what the feedback is about.

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

### What is in place

- **Setup:** the first admin can only be created with a one-time code from the server log (or via environment variables).
- **Passwords:** PBKDF2-SHA256 with 600,000 iterations and a random salt; login takes the same time for unknown usernames.
- **Brute force:** failed logins are limited per sender (8 per 15 min) and per username (20 per hour, independent of the IP).
- **Sessions:** HMAC-signed cookie, `HttpOnly`, `Secure` on HTTPS, `SameSite=Strict`, 14 days. Changing the password, deleting the user or *Log out everywhere else* ends all other sessions.
- **CSRF:** every state-changing request must be same-origin (`Sec-Fetch-Site`, else `Origin`/`Referer`).
- **XSS:** all output is escaped by Go’s `html/template`; strict Content Security Policy without inline scripts, no external sources; `X-Frame-Options: DENY`; HSTS on HTTPS.
- **Uploads:** type detected from the content, not the extension (PNG, JPEG, GIF, WebP, MP4, MOV, WebM, TXT/LOG); stored under random names; served with the detected type, `nosniff`, `Content-Disposition` and a sandboxing CSP; size limit per file, per request and in total.
- **Access:** attachments and status pages need the admin session or the 192-bit status token (stored only as a SHA-256 hash).
- **Spam:** proof of work, honeypot, minimum fill time, rate limit (see above).
- **Proxy headers** (`X-Forwarded-*`) are only trusted with `FL_TRUST_PROXY=true`.
- **Supply chain:** no third-party Go modules; distroless, non-root container.

### Known limitations

- Rate limits live in memory; a restart resets them.
- All admins have the same rights; there are no roles and no two-factor authentication yet.
- With `FL_TRUST_PROXY=true`, the app must only be reachable through your proxy (bind to `127.0.0.1` or the Docker bridge, never `0.0.0.0`), otherwise clients can forge their IP and host.
- `style-src` allows inline styles (needed for the white-label colors); scripts are not affected.
- Videos are stored as uploaded; metadata is not stripped from them.
- Data is stored unencrypted on disk; use disk encryption if you need encryption at rest.

### Operating it safely

1. Put it behind a reverse proxy with HTTPS (Caddy does this automatically).
2. Publish the port only locally (`FL_BIND`, default `127.0.0.1:8080`).
3. Rebuild regularly to pick up Go and base-image security fixes: `git pull && docker compose up -d --build`.
4. Back up the data volume.
5. Use strong, unique admin passwords.

### Reporting a vulnerability

Please don’t open a public issue. Use GitHub’s private vulnerability reporting instead – see [SECURITY.md](SECURITY.md).

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
