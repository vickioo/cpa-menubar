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
- Optional read-only Sub2 mode that returns only currently valid accounts.
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

The bridge does not return access tokens, refresh tokens, ID tokens, management keys, or provider account IDs. Account lists expose masked identifiers only. In Sub2 mode, an authenticated per-account detail request may return the complete Sub2 display name after the user selects that account; no credential fields are included.

### Sub2 read-only mode

Set `CPA_ACCOUNT_SOURCE=sub2` to read operational account status from Sub2 PostgreSQL instead of auth files. The bridge includes an account only when it is active, schedulable, unexpired, and not temporarily blocked. Focus is controlled manually by the Windows dashboard and remains in the local browser profile.

The Sub2 query uses an explicit column list. A database-owner view extracts only named upstream quota scalars from `credentials` and `extra`; the desktop reader cannot select either source column. Use the dedicated PostgreSQL login and restricted view in [bridge/deploy/sub2-readonly.sql](bridge/deploy/sub2-readonly.sql) and [bridge/deploy/sub2-account-usage-view.sql](bridge/deploy/sub2-account-usage-view.sql).

```dotenv
CPA_ACCOUNT_SOURCE=sub2
SUB2_DATABASE_URL=postgres://cpa_desktop_reader:REPLACE_ME@postgres:5432/sub2?sslmode=require
SUB2_TARGET_0703_ACCOUNT_ID=20
SUB2_TARGET_FU_ACCOUNT_IDS=2,24
```

In this mode credential mutation remains disabled: the desktop cannot add OAuth authorizations or consume reset credits. A separate authenticated refresh endpoint performs read-only OAuth liveness, quota, reset-credit-count, and subscription-expiry probes through a dedicated database role and restricted view.

For a Docker deployment beside an existing Sub2 stack, use:

```bash
bridge/deploy/deploy-sub2-docker.sh /path/to/cpa-menubar
```

The script builds a static bridge, creates or rotates a column-restricted PostgreSQL reader, binds the service to `127.0.0.1:18330`, and joins the existing `sub2api_sub2api-network`. Review its network, container, database, and focus-group defaults before use.

### Windows tray preview

Run `preview/start-tray.vbs` or use the generated desktop shortcut. The tray process:

- opens an SSH tunnel to the loopback-only bridge;
- starts the local credential-hiding proxy on `127.0.0.1:8765`;
- opens the dashboard on double-click;
- provides reconnect and exit actions from its context menu.

The dashboard only lists server-approved valid accounts. Stars are controlled manually and stored in the local browser profile; they do not write to Sub2. A quota-pressure filter can isolate accounts at 90% weekly usage without marking them as focused.

Account names stay masked in the list. Selecting a name requests a narrow authenticated detail endpoint that returns only the full Sub2 display name and public account ID.

The overview and tray menu use upstream quota values already cached by Sub2. The 0703 card reads the configured 20X primary account's native 7-day percentage and reset time. The FuCCC card sums the weekly limit and usage for its two configured API-key mirror accounts. These figures are not Sub2 group budgets or `usage_logs.actual_cost` totals.

Each account card uses its native 7-day or weekly quota as the primary progress bar. Expanding a selected account reveals 5-hour quota, reset time, bound groups, authorization type, plan, authorization probe state, available reset-credit count, subscription expiry, last use, and quota update time. The fixed refresh action probes OAuth accounts in real time; only HTTP 401/403 marks authorization invalid, while transient network failures remain probe errors. Automatic subscription expiry is preferred over the dashboard's optional local manual note and is displayed separately from authorization survival.

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
