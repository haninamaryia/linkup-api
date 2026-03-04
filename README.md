# Linkup API

A Go API for scheduling meetings and finding the best time that works for everyone. Organizers create events and invite participants by email; participants submit their availability via a share link; the API computes overlapping time slots.

## Features

- **Passwordless auth** — Email + one-time code; verify to get a JWT for organizer actions.
- **Events** — Create, list, get, update, delete events (organizer only). Optional time frame and participant emails.
- **Availability** — Submit time slots per event; get best time (first overlap) or best times (all overlaps).
- **Invitations** — Share link by token: get event details (no auth), submit availability (optional email to mark participant as responded).
- **Participant status** — Organizer can see who was invited and who has responded.

## Prerequisites

- Go 1.21+
- SQLite (file-based; no separate server)

## Run

```bash
# Build and run (default: http://:8080, DB file linkup.db)
go build -o linkup-api ./cmd && ./linkup-api
```

Or run directly:

```bash
go run ./cmd
```

### Environment variables

| Variable          | Default     | Description                    |
|-------------------|-------------|--------------------------------|
| `SERVER_ADDRESS`  | `:8080`     | HTTP listen address            |
| `DATABASE_URL`    | `linkup.db` | SQLite database file path      |
| `JWT_SECRET`      | `dev-secret`| Secret for signing JWTs        |

## API overview

| Area          | Endpoints |
|---------------|-----------|
| Auth          | `POST /auth/request-code`, `POST /auth/verify-code` |
| Events        | `POST /events`, `GET /events`, `GET /events/:id`, `PATCH /events/:id`, `DELETE /events/:id`, `GET /events/:id/participant-status` |
| Availability  | `POST /events/:id/availability`, `GET /events/:id/best-time`, `GET /events/:id/best-times` |
| Invitations   | `GET /inv/:token`, `POST /inv/:token/availability` |

Endpoints under `/events` (except `GET /events/:id`) and `GET /events/:id/participant-status` require a JWT in the `Authorization: Bearer <token>` header. Invitation endpoints do not require auth.

Base URL in examples: **`http://localhost:8080`**.

---

## Example requests

### 1. Auth

**Request a verification code** (creates or finds user; in dev the code is printed to server stdout):

```bash
curl -s -X POST http://localhost:8080/auth/request-code \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","name":"Alice"}'
```

Response: `{"message":"verification code sent"}`

**Verify code and get JWT** (use the code from server logs in dev):

```bash
curl -s -X POST http://localhost:8080/auth/verify-code \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","code":"<CODE>"}'
```

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
curl -s http://localhost:8080/events/1
```

**Update event** (partial; organizer only):

```bash
curl -s -X PATCH http://localhost:8080/events/1 \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"Updated title","participant_emails":["bob@example.com"]}'
```

**Delete event** (organizer only; cascades to availabilities, participants, invitations):

```bash
curl -s -X DELETE http://localhost:8080/events/1 -H "Authorization: Bearer $TOKEN"
```

**Participant status** (organizer only; responded_count, total_count, percent_responded):

```bash
curl -s http://localhost:8080/events/1/participant-status -H "Authorization: Bearer $TOKEN"
```

---

### 3. Availability

**Submit availability** for an event (optional `user_id` when authenticated):

```bash
curl -s -X POST http://localhost:8080/events/1/availability \
  -H "Content-Type: application/json" \
  -d '{
    "slot_start": "2025-03-10T10:00:00Z",
    "slot_end": "2025-03-10T11:00:00Z"
  }'
```

Response (201): `{"id":...,"event_id":1,"slot_start":"...","slot_end":"..."}`

**Get first best time** (one overlapping slot):

```bash
curl -s http://localhost:8080/events/1/best-time
```

Response: `{"slot_start":"...","slot_end":"..."}` or `{"message":"no overlapping availability found"}`

**Get all best times**:

```bash
curl -s http://localhost:8080/events/1/best-times
```

Response: `{"best_times":[{"slot_start":"...","slot_end":"..."},...]}` or `{"best_times":[],"message":"..."}`.

---

### 4. Invitations (participant flow; no auth)

**Get event by invitation token** (for share link page):

```bash
curl -s http://localhost:8080/inv/abc123def456...
```

Response: event details plus `organizer_name` (no share_link; used on participant landing page).

**Submit availability via invitation** (optional `email` to mark participant as responded):

```bash
curl -s -X POST http://localhost:8080/inv/abc123def456.../availability \
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
- **Auth**: One-time codes are stored in memory (single instance only). In production, use a persistent store or external provider for codes and set `JWT_SECRET` to a secure value.

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

API tests live in `http/api/routes_test.go` and cover auth, event CRUD, invitation flow, availability, best-times, and participant status.
