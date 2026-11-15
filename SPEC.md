# Technical Specification: SupportCenter Plus Telegram Bot (`bot-go`)

## 1. Overview & Goals

`bot-go` is a Go-based Telegram bot for internal IT/Support technicians to browse and inspect tickets in **ManageEngine SupportCenter Plus (On-Premise)** via private 1-on-1 Telegram DMs.

### 1.1 v1 Scope
- **Strictly read-only** ticket browsing and inspection on demand (no ticket mutations, no proactive notifications).
- **Per-technician effective RBAC**: Every ticket query runs using the technician's own SupportCenter `authtoken`, ensuring SupportCenter Plus enforces their exact role, site/account scope, and group visibility.
- **Inline-button SPA UX**: Single-message navigation with an inline filter picker, paginated request list, and ticket detail view.
- **Extensible architecture**: Pluggable sub-dialog/action registry on the ticket detail view to support v2 modules (Tasks, Notes & Conversations, Time Entries, Resolution, Image Viewing, Write Actions, Notifications).

---

## 2. Technology Stack

- **Language**: Go `1.25.5` (`github.com/s2gamb/bot-go`)
- **Telegram Bot Framework**: `github.com/go-telegram/bot` (`v1.28.0+`)
- **SupportCenter Plus Client**: `github.com/s2gamb/sdp-go` (update to `v0.0.0-20261114120450-2b1370faa190` or newer, which includes `TechniciansService` and `SearchCriterion`)
- **Database**: Embedded SQLite via pure-Go `modernc.org/sqlite` (no CGO required)
- **Encryption**: AES-256-GCM for encrypting per-technician `authtoken` values at rest

---

## 3. Architecture & Package Layout

```text
bot-go/
├── cmd/
│   └── bot/
│       └── main.go              # Entrypoint, wiring, graceful shutdown, polling/webhook startup
├── internal/
│   ├── config/
│   │   └── config.go            # Environment-based configuration & validation
│   ├── crypto/
│   │   └── aesgcm.go            # AES-256-GCM encrypt/decrypt helpers
│   ├── store/
│   │   ├── sqlite.go            # SQLite connection, migrations
│   │   └── technicians.go       # CRUD for linked technician records & encrypted tokens
│   ├── sdp/
│   │   ├── service.go           # Wrapper around sdp-go (service client + per-user client factory)
│   │   └── filters.go           # Mapping UI filter presets -> sdp.ListOptions / SearchCriterion
│   ├── bot/
│   │   ├── router.go            # Command & callback_data routing + auth middleware
│   │   ├── auth_handler.go      # /auth <token>, /logout, identity verification flow
│   │   ├── list_handler.go      # /start, request list rendering, paginator, filter picker dialog
│   │   ├── detail_handler.go    # Ticket detail view + pluggable action registry
│   │   └── callback.go          # Compact callback_data encoder/decoder (<= 64 bytes)
│   └── format/
│       ├── html.go              # SupportCenter HTML -> Telegram HTML converter & [Image] placeholder
│       └── ticket.go            # Ticket list button labels & detail message formatter (<= 4096 chars)
├── go.mod
├── go.sum
└── SPEC.md
```

---

## 4. Authentication, Authorization & Identity Mapping

### 4.1 Two-Token Security Model
1. **Service Token (`SDP_SERVICE_TOKEN`)**:
   - Used **only** for identity verification: looking up a technician in SupportCenter Plus by their Telegram User ID stored in a configured additional field (`SDP_TELEGRAM_FIELD`) via `Technicians.FindByField`.
   - Never used to fetch or display tickets to users.
2. **Technician Token (User-Provided)**:
   - Each technician runs `/auth <authtoken>` in a private DM with the bot.
   - The bot **immediately deletes** the incoming `/auth` message from the chat so the raw token does not linger in chat history.
   - The bot verifies:
     1. The Telegram user's `From.ID` matches a technician in SupportCenter Plus (`Technicians.FindByField(ctx, cfg.SDPTelegramField, strconv.FormatInt(userID, 10))`).
     2. The provided `authtoken` is valid by making a lightweight authenticated call to SupportCenter Plus (e.g., `Requests.List` with `RowCount: 1` or `Technicians.Get` for their own ID).
   - The token is encrypted with AES-256-GCM (`TOKEN_ENCRYPTION_KEY`) and stored in SQLite alongside `telegram_user_id`, `sdp_technician_id`, `sdp_technician_name`, and `updated_at`.

### 4.2 Chat Topology & Middleware
- **Private DMs only**: Updates from groups, supergroups, or channels are ignored (or politely rejected).
- **Auth Middleware**: Every command/callback (except `/auth` and `/help`) checks:
  - Does `telegram_user_id` exist in SQLite with a stored token?
  - If not, prompt the user to authenticate via `/auth <your_api_token>` (only if their Telegram ID is registered in SupportCenter, or give a generic unauthorized message if not found).
  - If a SupportCenter API call returns `401 Unauthorized` / invalid token, prompt the technician to re-authenticate with `/auth <new_token>`.

### 4.3 Commands
- `/start` — Opens the main ticket list view (default filter: **My Open Tickets**, page 1).
- `/auth <token>` — Links and validates the technician's SupportCenter `authtoken` (deletes the user's message).
- `/logout` — Removes the stored encrypted token from SQLite.
- `/help` — Brief usage instructions.

---

## 5. UX & Navigation Flow

All browsing happens in-place by editing the active bot message (`EditMessageText`) via inline keyboards.

### 5.1 Callback Data Encoding
Telegram limits `callback_data` to **64 bytes**. Use a compact, versioned/delimited format:
- `l:<filter>:<page>` — Render ticket list (e.g., `l:my:1`, `l:un:2`, `l:grp:1`)
- `fp:<page>` — Open Filter Picker dialog (preserving current page or resetting to 1 on selection)
- `d:<ticket_id>:<filter>:<page>` — Open Ticket Detail View (retaining return context for `« Back`)
- `noop` — Non-interactive indicator button (e.g., `Page 1/5`)

### 5.2 Main Ticket List View (`/start` / `l:<filter>:<page>`)
- **Message Header**: Active filter name, total/page indicator (e.g., `📋 My Open Tickets — Page 1`).
- **Inline Keyboard Layout**:
  1. **Top Row (Filter Picker)**: `[ 🔍 Filter: My Open Tickets ▾ ]` (`callback_data: fp:<page>`)
  2. **Middle Rows (Tickets)**: Up to `REQUESTS_PER_PAGE` rows, 1 button per ticket:
     - Label format: `#<ID> · <Subject>` (truncated cleanly to fit mobile screens, e.g., ~40–50 chars, optionally prefixed with priority/status indicator).
     - `callback_data: d:<id>:<filter>:<page>`
  3. **Bottom Row (Paginator)**:
     - `[ « Prev ]` (omitted or `noop` on page 1) | `[ Page N ]` (`noop`) | `[ Next » ]` (omitted or `noop` when `!ListInfo.HasMoreRows`)

### 5.3 Filter Picker Dialog (`fp:<page>`)
Edits the current message to display filter choices as inline buttons:
- `[ ✓ My Open Tickets ]` (`l:my:1`) — Default on `/start`. Requests assigned to the current technician with open/non-closed status.
- `[ Unassigned Tickets ]` (`l:un:1`) — Open requests with no assigned technician.
- `[ All My Groups ]` (`l:grp:1`) — Open requests belonging to any technician group the user is a member of (resolved from their Technician record or SupportCenter filter).
- `[ « Cancel ]` (`l:<current_filter>:<page>`)

*(Exact `SearchFields` / `FilterBy` criteria will be verified against the SupportCenter Plus On-Premise v3 API responses during implementation.)*

### 5.4 Ticket Detail View (`d:<id>:<filter>:<page>`)
Fetches `Requests.Get(ctx, id)` using the technician's own `authtoken`.

- **Message Body** (Telegram HTML parse mode, capped at 4096 chars):
  - **Header**: `#<ID> — <Subject>` + clickable link to the ticket in SupportCenter web UI (`<SDP_BASE_URL>/WorkOrder.do?woMode=viewWO&woID=<ID>` or configurable URL template).
  - **Metadata**: Status, Priority, Group, Assigned Technician, Requester, Created Time, Due By Time.
  - **Description**:
    - Sanitized from SupportCenter HTML into safe Telegram HTML (`<b>`, `<i>`, `<code>`, `<pre>`, `<a>`, newlines).
    - `<img>` tags replaced with `[Image]` placeholders in v1.
    - Truncated gracefully with `… (truncated)` if the total message approaches Telegram's 4096-character limit.
- **Inline Keyboard**:
  - **Pluggable Action Rows (v2 extension point)**:
    - Defined via a `DetailActionProvider` interface/registry (e.g., `Tasks`, `Notes & Conversations`, `Time Entries`, `Resolution`, `View Images`).
    - Empty in v1; adding a v2 sub-dialog only requires registering a new provider and callback prefix without modifying list/detail core logic.
  - **Bottom Navigation Row**: `[ 🔄 Refresh ]` (`d:<id>:<filter>:<page>`) | `[ « Back to List ]` (`l:<filter>:<page>`)

---

## 6. Database Schema (SQLite)

```sql
CREATE TABLE IF NOT EXISTS technicians (
    telegram_user_id    INTEGER PRIMARY KEY,
    sdp_technician_id   TEXT NOT NULL,
    sdp_technician_name TEXT NOT NULL,
    encrypted_token     BLOB NOT NULL,
    nonce               BLOB NOT NULL,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- `encrypted_token` + `nonce`: AES-256-GCM ciphertext of the technician's SupportCenter `authtoken`.
- `TOKEN_ENCRYPTION_KEY`: 32-byte key (hex or base64 encoded) provided via environment variable.

---

## 7. Configuration & Deployment

All configuration is loaded from environment variables (with sensible defaults where appropriate):

| Variable | Required | Default | Description |
| :--- | :---: | :--- | :--- |
| `TELEGRAM_BOT_TOKEN` | Yes | — | Telegram Bot API token from `@BotFather` |
| `SDP_BASE_URL` | Yes | — | SupportCenter Plus base URL (e.g., `https://support.company.local`) |
| `SDP_SERVICE_TOKEN` | Yes | — | Service `authtoken` used **only** for `Technicians.FindByField` identity lookup |
| `SDP_TELEGRAM_FIELD` | Yes | — | Additional field key on Technician in SupportCenter storing Telegram User ID |
| `TOKEN_ENCRYPTION_KEY` | Yes | — | 32-byte hex/base64 key for AES-256-GCM token encryption |
| `DB_PATH` | No | `./data/bot.db` | Path to SQLite database file |
| `REQUESTS_PER_PAGE` | No | `8` | Number of request buttons per page |
| `BOT_MODE` | No | `polling` | Update mode: `polling` (default) or `webhook` |
| `WEBHOOK_URL` | If webhook | — | Public HTTPS URL for Telegram webhook |
| `WEBHOOK_LISTEN_ADDR` | No | `:8443` | HTTP listen address for webhook server |
| `WEBHOOK_SECRET` | No | — | Optional `X-Telegram-Bot-Api-Secret-Token` |
| `SDP_INSECURE_TLS` | No | `false` | Optional flag for on-prem self-signed certificates |
| `LOG_LEVEL` | No | `info` | Structured logging level (`slog`: `debug`, `info`, `warn`, `error`) |

---

## 8. Future Roadmap (v2+)
Designed to plug cleanly into the v1 architecture:
- **Pluggable Detail Sub-Dialogs**: `Tasks`, `Notes & Conversations`, `Time Entries`, `Resolution`.
- **Image Handling**: Download inline/attached images via technician `authtoken` and send as Telegram media groups.
- **Write Actions**: Assign to me, change status/priority, add public reply or private note.
- **Proactive Notifications**: SupportCenter webhooks or polling for newly assigned tickets and SLA warnings.
