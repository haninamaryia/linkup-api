# Linkup API

A Go API for scheduling meetings and finding the best time that works for everyone. Organizers create events and invite participants by email; participants submit their availability via a share link; the API computes overlapping time slots.

## Features

- **Passwordless auth** — Email + one-time code; verify to get a JWT for organizer actions.
- **Events** — Create, list, get, update, delete events (organizer only). Optional time frame and participant emails.
- **Availability** — Submit time slots per event; get best time (first overlap) or best times (all overlaps).
- **Invitations** — Share link by token: get event details (no auth), submit availability (optional email to mark participant as responded).
- **Participant status** — Organizer sees who was invited, who has responded, and **each participant’s submitted slots** (who submitted which times).

- **API contract** — OpenAPI 3.0 spec and interactive Swagger UI at `/docs` for testing, demos, and frontend integration.

## Prerequisites

- Go 1.21+
- SQLite (file-based; no separate server)

## Run

```bash
# Build and run (default: http://:8181, DB file linkup.db)
go build -o linkup-api ./cmd && ./linkup-api
```

Or run directly:

```bash
go run ./cmd
```

### Configuration

Config is loaded with **viper**: defaults are set in `cmd/main.go`, optional file **`cmd/config.yml`** overrides them, and **environment variables** override both. Use prefix `LINKUP_` and underscore instead of dot (e.g. `server.address` → `LINKUP_SERVER_ADDRESS`). You can set `CONFIG_PATH` to point to a directory containing `config.yml` (default: `cmd`).

| Env (LINKUP_*)     | Config key       | Default     | Description                    |
|--------------------|------------------|-------------|--------------------------------|
| `SERVER_ADDRESS`   | `server.address` | `:8181`     | HTTP listen address            |
| `DATABASE_URL`     | `database.url`   | `linkup.db` | SQLite database file path      |
| `JWT_SECRET`       | `jwt.secret`     | `dev-secret`| Secret for signing JWTs        |
| `CORS_ORIGINS`     | `cors.origins`   | `http://localhost:3000,...` | Comma-separated origins for CORS |
| `AUTH_CODE_TTL`    | `auth.code_ttl`  | `10m`       | Verification code expiry (e.g. `10m`, `1h`) |
| `AUTH_CODE_RATE_LIMIT_MAX` | `auth.code_rate_limit_max` | `3` | Max code requests per email per window (0 = disable) |
| `AUTH_CODE_RATE_LIMIT_WINDOW` | `auth.code_rate_limit_window` | `15m` | Rate limit window (e.g. `15m`, `1h`) |
| `EMAIL_PROVIDER`    | `email.provider` | `stub`      | Email sender: `stub` (log only) or future `resend`/`sendgrid` |

**Database:** The `participants` table has a unique constraint on `(event_id, email)` so the same email cannot appear twice for one event. Auth codes are stored in the `auth_codes` table with a 10-minute TTL and are removed on successful verify or by cleanup. If you had an older DB with a different schema, remove `linkup.db` (and `linkup.db-journal` if present) for a fresh start.

### Testing

**Makefile (unit, integration, smoke, pre-commit)**

| Target | Description |
|--------|-------------|
| `make test-unit` | Unit tests only (e.g. `./services/...`); pure logic, no DB/HTTP. |
| `make test-integration` | Integration tests (`./http/api/...`); handlers + in-memory DB, **mocks only** (no external HTTP or email). |
| `make test-smoke` | Smoke tests (**Dredd**) against a **live server**; set `BASE_URL` (default `http://localhost:8181`). Contracts in `docs/smoke-contracts.yaml`. Uses **npx** if available, else runs Dredd in Docker (no Node required). |
| `make pre-commit` | Runs unit → integration → starts Docker container → Dredd smoke → stops container. |

Smoke contracts are defined in **`docs/smoke-contracts.yaml`** (OpenAPI 3). Run Dredd manually: `npm install` then `npx dredd docs/smoke-contracts.yaml http://localhost:8181`. If you don't have Node, **`make test-smoke`** will run Dredd inside Docker (after starting the API with **`make docker-up`**).

**Health contract (Docker/K8s)**

| Endpoint | Contract |
|----------|----------|
| `GET /health` | 200, `{"status":"ok"}` — liveness (no dependencies checked). |
| `GET /ready` | 200, `{"status":"ok"}` when DB is reachable; 503 otherwise — readiness. |

**Automated tests**

```bash
go test ./...
```

Tests cover auth flow (codes in DB, verify via HTTP), CORS (allowed origin gets `Access-Control-Allow-Origin`), and availability validation (`slot_end` &gt; `slot_start`, slot within event timeframe returns clear errors).

**Manual checks**

- **Auth persistence (restart-safe):** Start the server, request a code (`POST /auth/request-code`), note the code from server logs. Restart the server. Call `POST /auth/verify-code` with the same email and code — you should get a JWT. If codes were in-memory only, verify would fail after restart.
- **CORS:** From a browser or a request with `Origin: http://localhost:3000`, call the API (e.g. `GET /events` with a valid JWT). Response should include `Access-Control-Allow-Origin: http://localhost:3000`. Without CORS config, the browser would block the response.
- **Validation:**  
  - `slot_end` ≤ `slot_start`: `POST /events/:id/availability` with `slot_end` before `slot_start` → 400 and `"slot_end must be after slot_start"`.  
  - Slot outside timeframe: Create an event with `time_frame_start` / `time_frame_end`, then submit a slot that starts before or ends after that window → 400 with a message that includes the allowed bounds.  
  - **Slot shorter than event duration:** Rejected with 400 (slot must fit a full meeting).  
  - **Reversed time frame:** `time_frame_end` before `time_frame_start` → 400.  
  - **Overlapping slots** in one invite request (slots array): 400 with "slot N overlaps slot M".

### Edge cases (handled)

| Case | Behavior |
|------|----------|
| Participant invited twice in update | List deduped; one row per email. |
| Participant removed after submitting slots | Their availability is deleted when removed from the list. |
| Event duration longer than submitted slot | 400: slot length must be ≥ event duration. |
| Invalid or reversed time_frame_start/end | 400: time_frame_end must be after time_frame_start. |
| Overlapping slots in one invite request | 400: "slot N overlaps slot M". |
| No participants vs anonymous | Both supported; anonymous = slots with no email. |
| Auth code replay | Code deleted on verify; replay returns 401 invalid/expired. |
| Expired invite token | GET/POST with token return 410 Gone. |
| Deleting event with responses | Cascade: availabilities, participants, invitations deleted. |

### API documentation

- **OpenAPI contract** — The API is described by an OpenAPI 3.0 spec. With the server running:
  - **Interactive docs (Swagger UI):** [http://localhost:8181/docs](http://localhost:8181/docs) — try requests from the browser, authorize with a JWT from `/auth/verify-code`.
  - **Raw spec:** [http://localhost:8181/openapi.yaml](http://localhost:8181/openapi.yaml) — for codegen, frontend types, or import into Postman/Insomnia.
- **Markdown overview** — See **`docs/api.md`** for endpoint list, request/response shapes, status codes, and edge-case summary.

### Seed data (local dev)

```bash
# Append seed user + event + invitation (creates if not present)
go run ./cmd/seed

# Reset DB and seed from scratch (destructive)
go run ./cmd/seed --reset
```

Uses `DATABASE_URL` (default `linkup.db`). Creates user `dev@localhost`, event "Seed Meeting", and share link `/inv/seed-invite-token-<id>`.

### Docker

```bash
make docker-build    # Build image
make docker-run      # Run in foreground (port 8181, DB in ./data)
make docker-up       # Start in background (for smoke tests)
make docker-down     # Stop and remove container
```

Smoke tests expect a running server: `make docker-up && make test-smoke && make docker-down`, or use `make pre-commit`.

## API overview

| Area          | Endpoints |
|---------------|-----------|
| Auth          | `POST /auth/request-code`, `POST /auth/verify-code` |
| Events        | `POST /events`, `GET /events`, `GET /events/:id`, `PATCH /events/:id`, `DELETE /events/:id`, `GET /events/:id/participant-status`, **`GET /events/:id/summary`** |
| Availability  | `POST /events/:id/availability`, `GET /events/:id/best-time`, `GET /events/:id/best-times` |
| Invitations   | `GET /inv/:token`, `POST /inv/:token/availability` |

Endpoints under `/events` (except `GET /events/:id`) and `GET /events/:id/participant-status` and `GET /events/:id/summary` require a JWT in the `Authorization: Bearer <token>` header. Invitation endpoints do not require auth.

Base URL in examples: **`http://localhost:8181`**.

### API conventions

- **Errors** — Every error response has the same shape: `{"error": "<message>"}`. The frontend can always read `error` for the message.
- **IDs** — All IDs in JSON are numbers (event `id`, `participant_id`, `creator_id`, `excluded_participant_ids`, etc.). No string IDs.
- **Slot shape** — Any slot object uses `slot_start` and `slot_end` (RFC3339 strings). In `best-time`, `best-times`, and summary, each slot also has `available_count` and `total`. Response field names are **snake_case** everywhere (e.g. `slot_start`, `event_id`, `responded_count`).
- **Invite flow when email is omitted** — Submitting a **single slot** without `email` adds one **anonymous** slot (not tied to a participant). Each such POST adds another anonymous slot; they do not replace each other. To identify or replace a participant’s slots, include `email` (and for multiple slots use the `slots` array; `email` is then required).
- **Organizer and availability** — The **organizer may submit their own availability** via `POST /events/:id/availability` (with or without JWT; optionally send `user_id` to attach slots to their user). Same validation and behavior as for other users.
- **Timezones** — All date-times in the API are **UTC** (RFC3339, e.g. `2006-01-02T15:04:05Z`). The frontend should convert to the user’s local timezone for display; store and send UTC in API requests.

---

## Example requests

### 1. Auth

**Request a verification code** (creates or finds user; in dev the code is printed to server stdout):

```bash
curl -s -X POST http://localhost:8181/auth/request-code \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","name":"Alice"}'
```

Response: `{"message":"verification code sent"}`

**Verify code and get JWT** (use the code from server logs in dev):

```bash
curl -s -X POST http://localhost:8181/auth/verify-code \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","code":"<CODE>"}'
```}'

Response: `{"token":"<JWT>"}`

Use this token in subsequent requests: `Authorization: Bearer <JWT>`.

---

### 2. Events (organizer; require JWT)

**Create event** (optional: description, location, time_frame_start/end as ISO8601, participant_emails):

```bash
export TOKEN="<your-jwt>"

curl -s -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "title": "Team sync",
    "description": "Weekly standup",
    "duration_minutes": 30,
    "time_frame_start": "2025-03-10T09:00:00Z",
    "time_frame_end": "2025-03-14T17:00:00Z",
    "participant_emails": ["bob@example.com", "carol@example.com"]
  }'
```

Response (201): event object with `id`, `share_link` (e.g. `/inv/abc123...`), `title`, `duration_minutes`, `time_frame_start`, `time_frame_end`, `created_at`, etc.

**List my events**

```bash
curl -s http://localhost:8080/events -H "Authorization: Bearer $TOKEN"
```

**Get single event** (no auth required; returns event + share_link):

```bash
curl -s http://localhost:8181/events/1
```

**Update event** (partial; organizer only):

```bash
curl -s -X PATCH http://localhost:8181/events/1 \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"Updated title","participant_emails":["bob@example.com"]}'
```

**Delete event** (organizer only; cascades to availabilities, participants, invitations):

```bash
curl -s -X DELETE http://localhost:8080/events/1 -H "Authorization: Bearer $TOKEN"
```

**Participant status** (organizer only; includes **per-participant slots** — who submitted which times):

```bash
curl -s http://localhost:8080/events/1/participant-status -H "Authorization: Bearer $TOKEN"
```

Response: `participants` (each with `id`, `email`, `responded`, `responded_at`, **`slots`** array of `{slot_start, slot_end}`), `responded_count`, `total_count`, `percent_responded`.

**Event summary** (organizer only; **one endpoint for the full event page** — event details, participants with slots, and top 1–3 best-time recommendations):

```bash
curl -s http://localhost:8181/events/1/summary -H "Authorization: Bearer $TOKEN"
```

Response (200) — **stable shape**:

| Field | Type | Description |
|-------|------|-------------|
| `event` | object | Same as single-event response: `id`, `creator_id`, `title`, `description`, `location`, `duration_minutes`, `time_frame_start`, `time_frame_end`, `share_link`, `created_at` |
| `participants` | array | Each item: `id`, `email`, `responded`, `responded_at`, `slots` (array of `{slot_start, slot_end}`) |
| `responded_count` | number | Number of participants who have submitted availability |
| `total_count` | number | Total number of participants |
| `percent_responded` | number | 0–100 |
| `best_times` | array | Top 1–3 recommended slots. Each: `slot_start`, `slot_end`, `available_count`, `total` |
| `note` | string (optional) | Set when no slot fits everyone (e.g. fallback message) |
| `excluded_participant_ids` | array (optional) | Participant IDs excluded from the top recommendation when `note` is set |

Frontends can build the event dashboard with **2–3 API calls total**: auth (request-code + verify-code), optional `GET /events` to list, then **`GET /events/:id/summary`** to render the full event page.

---

### 3. Availability

**Submit availability** for an event (optional `user_id` when authenticated):

```bash
curl -s -X POST http://localhost:8181/events/1/availability \
  -H "Content-Type: application/json" \
  -d '{
    "slot_start": "2025-03-10T10:00:00Z",
    "slot_end": "2025-03-10T11:00:00Z"
  }'
```

Response (201): `{"id":...,"event_id":1,"slot_start":"...","slot_end":"..."}`

**Get first best time** (one overlapping slot):

```bash
curl -s http://localhost:8181/events/1/best-time
```

Response: `{"slot_start":"...","slot_end":"..."}` or `{"message":"no overlapping availability found"}`

**Get all best times** — Within the organizer’s time frame (if set), finds **all** possible meeting slots of the event’s duration where **all participants** are available. Availability is grouped by participant (each participant’s slots are merged); the intersection of all participants’ availability is used. Slots are emitted in **30-minute steps** (e.g. 12:00–12:45, 12:30–13:15 for a 45‑min meeting). If there is no slot where everyone is available, the API falls back to slots where **most** participants are available and returns `note` and `excluded_participant_ids`.

```bash
curl -s http://localhost:8181/events/1/best-times
```

Response: `{"best_times":[{"slot_start":"...","slot_end":"...","available_count":n,"total":m},...]}` or `{"best_times":[],"message":"..."}`. Both `GET /events/:id/best-time` and `GET /events/:id/best-times` use the same `best_times` array shape; best-time returns at most one slot. When fallback is used: `"note": "..."` and `"excluded_participant_ids": [<id>]`.

---

### 4. Invitations (participant flow; no auth)

**Get event by invitation token** (for share link page):

```bash
curl -s http://localhost:8181/inv/abc123def456...
```

Response: event details plus `organizer_name` (no share_link; used on participant landing page).

**Submit availability via invitation** — When `email` is provided: the participant is looked up by event + email (or created if new), the submission is stored with `participant_id`, and the participant is marked as responded. Without email the slot is stored anonymously.

```bash
curl -s -X POST http://localhost:8181/inv/abc123def456.../availability \
  -H "Content-Type: application/json" \
  -d '{
    "email": "bob@example.com",
    "slot_start": "2025-03-10T10:00:00Z",
    "slot_end": "2025-03-10T11:00:00Z"
  }'
```

Response (201): `{"message":"availability submitted","id":...}`

---

## Data formats

- **Dates/times**: ISO 8601 / RFC3339 (e.g. `2025-03-10T10:00:00Z`).
- **Errors**: JSON object with `"error": "message"`.
- **Auth**: One-time codes are stored in SQLite (`auth_codes` table) with a 10-minute TTL; server restarts do not break login. Set `JWT_SECRET` to a secure value in production.

## Project layout

```
cmd/           — Entrypoint (main)
config/        — Env-based config
database/      — SQLite connection and migrations
models/        — GORM entities (User, Event, Availability, Invitation, Participant)
services/      — Auth, scheduling, participants, invitations, email (stub)
http/api/      — HTTP handlers and route registration
utils/         — JWT helpers
```

## Tests

```bash
go test ./...
```

API tests live in `http/api/routes_test.go` and cover auth, event CRUD, invitation flow, availability, best-times, and participant status. Scheduling logic (intersection by participant, 30‑min step, frame clipping, fallback when not all available) is tested in `services/scheduling_test.go`.
