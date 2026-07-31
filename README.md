# CPA Menu

CPA Menu is a native macOS menu-bar dashboard for monitoring CLIProxyAPI-compatible accounts, quota windows, reset credits, OAuth status, and optional xAI billing data.

The repository contains:

- `app/`: a SwiftUI menu-bar application for macOS 14 or later.
- `bridge/`: a small Go service that reads local provider auth files and exposes a narrow desktop API.
- `third-party/`: license texts for referenced open-source projects.

## Features

- Compact tabbed dashboard with independently scrollable Overview, Accounts, and xAI views.
- Account filtering for all, Plus, K12, and unhealthy accounts.
- Weekly quota, recharge balance, reset-credit availability, and refresh-token status.
- Two-step confirmation before consuming an irreversible reset credit.
- Optional xAI Management API billing totals and prepaid-credit balance.
- Separate operational telemetry from a compatible smart-router summary endpoint.
- Codex PKCE login from the menu bar, with credentials written directly to the configured auth directory.
- Optional read-only Sub2 mode that returns only valid or explicitly focused accounts.
- Desktop token stored only in a remote `0600` file and the local macOS Keychain.
- Configurable auto-refresh and native launch-at-login support.

## Architecture

```text
CPA Menu.app
  ├─ macOS Keychain: desktop bridge token
  ├─ HTTPS /desktop/v1/*
  └─ localhost:1455 OAuth callback
          │
          ▼
your HTTPS reverse proxy
          │
          ▼
cpa-desktop-bridge :8330
  ├─ scans /var/lib/cliproxy/auth/*.json
  ├─ queries provider usage with existing access tokens
  ├─ optionally reads xAI Management API settings
  ├─ optionally reads smart-router operational summaries
  └─ atomically writes auth JSON only after OAuth completion
```

The bridge does not return access tokens, refresh tokens, ID tokens, management keys, complete email addresses, or provider account IDs. Usage responses expose only masked identifiers, aggregate amounts, token counts, and status fields.

### Sub2 read-only mode

Set `CPA_ACCOUNT_SOURCE=sub2` to read operational account status from Sub2 PostgreSQL instead of auth files. The bridge includes an account only when it is currently valid, or when it is marked as important by priority, `[focus]` / `[重点]` in notes, or a configured focus group.

The Sub2 query uses an explicit column list and never selects `credentials`, `extra`, tokens, passwords, or complete provider identifiers. Use a dedicated PostgreSQL login with column-level grants; see [bridge/deploy/sub2-readonly.sql](bridge/deploy/sub2-readonly.sql).

```dotenv
CPA_ACCOUNT_SOURCE=sub2
SUB2_DATABASE_URL=postgres://cpa_desktop_reader:REPLACE_ME@postgres:5432/sub2?sslmode=require
SUB2_FOCUS_PRIORITY=80
SUB2_FOCUS_GROUP_IDS=
```

In this mode OAuth and reset-credit endpoints are disabled, and the app hides their controls.

For a Docker deployment beside an existing Sub2 stack, use:

```bash
bridge/deploy/deploy-sub2-docker.sh /path/to/cpa-menubar
```

The script builds a static bridge, creates or rotates a column-restricted PostgreSQL reader, binds the service to `127.0.0.1:18330`, and joins the existing `sub2api_sub2api-network`. Review its network, container, database, and focus-group defaults before use.

## Configuration

Copy the example and replace every placeholder locally. Never commit the resulting file.

```bash
cp bridge/deploy/cpa-desktop-bridge.env.example bridge/deploy/cpa-desktop-bridge.env
```

The systemd example uses these generic paths:

- Auth directory: `/var/lib/cliproxy/auth`
- Desktop token: `/etc/cpa-desktop-bridge.token`
- Router settings: `/etc/cpa-desktop-bridge/router.env`
- xAI billing settings: `/etc/cpa-desktop-bridge/xai-management.env`

Adjust `bridge/deploy/cpa-desktop-bridge.service` before deployment if your paths differ.

## Build and Test

```bash
cd bridge
go test -race ./...
go vet ./...

cd ../app
swift test
./scripts/build-app.sh
```

The app bundle is written to `app/dist/CPA Menu.app`. Build caches and app bundles are excluded from Git.

## Deploy

Deploy the bridge to an SSH host:

```bash
bridge/deploy/deploy.sh your-server
```

The script installs the service and an nginx location snippet without modifying an existing virtual host. Add the following line to your HTTPS server block, validate the configuration, and reload nginx:

```nginx
include /etc/nginx/snippets/cpa-desktop-bridge.conf;
```

Configure the macOS app with your own HTTPS endpoint:

```bash
app/scripts/configure-production.sh your-server https://cpa.example.com/desktop/v1
app/scripts/install-app.sh
```

Replace the example hostname with the endpoint you control.

## Security Boundaries

- Keep all desktop, OAuth, router, and xAI secrets outside Git.
- Bind the bridge to loopback and expose it only through an authenticated HTTPS reverse proxy.
- Keep OAuth state and PKCE verifiers in memory with short expiration times.
- Require both UI confirmation and server-side availability checks before consuming reset credits.
- Store auth files with `0600` permissions and update them using same-directory atomic rename.
- Review the service paths and permissions before deploying to a production host.

See `SECURITY.md` for reporting and deployment guidance.

## License

CPA Menu is available under the MIT License. See `LICENSE` and `THIRD_PARTY_NOTICES.md`.
