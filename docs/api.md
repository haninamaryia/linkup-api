# Linkup API – OpenAPI 3.0 overview

Base URL: `http://localhost:8181` (or `SERVER_ADDRESS`).

All error responses: `{"error": "<string>"}`. Success responses use the shapes below.

## Auth

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | /auth/request-code | No | Body: `{ "email", "name" }`. Returns `{ "message" }`. 429 if rate limited. |
| POST | /auth/verify-code | No | Body: `{ "email", "code" }`. Returns `{ "token" }`. 401 invalid/expired code. |

**Rate limiting:** Configurable max requests per email per window (e.g. 3 per 15m). 429 = too many code requests.

## Events (organizer)

| Method | Path | Description |
|--------|------|-------------|
| POST | /events | Body: `title`, optional `description`, `location`, `duration_minutes`, `time_frame_start`, `time_frame_end` (RFC3339; end must be after start), `participant_emails`. Returns event + `share_link`. |
| GET | /events | List organizer’s events. |
| GET | /events/:id | Public. Event + share_link. |
| PATCH | /events/:id | Partial update. `participant_emails`: replaces list; removed participants lose their availability. Duplicate emails in list deduped. |
| DELETE | /events/:id | Cascade: availabilities, participants, invitations. 204. |
| GET | /events/:id/participant-status | `participants`, `responded_count`, `total_count`, `percent_responded`. |
| GET | /events/:id/summary | Event, participants with slots, top 1–3 `best_times`, counts. |

**Validation:** `time_frame_end` must be after `time_frame_start`. Slot length must be ≥ event duration.

## Availability

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | /events/:id/availability | Optional | Body: `slot_start`, `slot_end` (RFC3339), optional `user_id`. Slot must be within event timeframe (if set) and length ≥ duration. |
| GET | /events/:id/best-time | Optional | `{ "best_times": [ { "slot_start", "slot_end", "available_count", "total" } ], "message"?, "note"?, "excluded_participant_ids"? }`. |
| GET | /events/:id/best-times | Optional | Same shape; array may have multiple slots. |

## Invitations (participant flow)

| Method | Path | Description |
|--------|------|-------------|
| GET | /inv/:token | Event details for share link. 404 not found, 410 invitation expired. |
| POST | /inv/:token/availability | Body: optional `email`, single `slot_start`/`slot_end` or `slots` array (email required). With email: replace that participant’s slots. Overlapping slots in one request rejected. 410 if invite expired. |

**Invite expiry:** Tokens have optional `ExpiresAt` (e.g. 30 days). Expired → 410 Gone.

## HTTP status codes

| Code | Meaning |
|------|---------|
| 200 | OK |
| 201 | Created |
| 204 | No Content (delete) |
| 400 | Bad Request (validation, invalid body) |
| 401 | Unauthorized (missing/invalid JWT or code) |
| 403 | Forbidden (not event creator) |
| 404 | Not Found (event, invitation) |
| 410 | Gone (invitation expired) |
| 429 | Too Many Requests (auth code rate limit) |
| 500 | Internal Server Error |

## Edge cases (handled)

- **Participant invited twice in update:** List is deduped; one row per email.
- **Participant removed after submitting slots:** Their availability rows are deleted when they are removed from the list.
- **Event duration longer than slot:** Rejected; slot length must be ≥ event duration.
- **Invalid or reversed time_frame_start/end:** Rejected with clear error.
- **Overlapping slots in one request (invite flow, slots array):** Rejected (e.g. "slot 2 overlaps slot 1").
- **No participants vs anonymous:** No participants = empty list; anonymous = slots with `participant_id` null; both supported.
- **Auth code replay:** Code is deleted on successful verify; replay returns invalid/expired.
- **Expired invite token:** GET/POST with that token return 410.
- **Deleting event with responses:** Cascade deletes all related data (availabilities, participants, invitations).
