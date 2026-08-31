# LinkedIn Profile API

A backend-first Go service that accepts a LinkedIn profile URL plus the caller's LinkedIn session and returns the information visible to that account as structured JSON. The same binary serves a small browser UI, the API, and its OpenAPI contract.

> LinkedIn does not provide this endpoint as a public API. The implementation can break when LinkedIn changes its internal Voyager API, and automated access may violate LinkedIn's terms. Use only an account and profile data you are authorized to access. The service does not bypass privacy controls, login challenges, or CAPTCHA.

## Features

- Name, headline, location, about, profile images
- Experience with grouped company positions
- Education, skills, certifications, and languages
- Strict LinkedIn URL validation
- API-key authentication and per-client token-bucket limiting
- Bounded concurrency and conservative LinkedIn request pacing
- Session-isolated in-memory cache and duplicate-request coalescing
- Partial-result metadata when LinkedIn omits a section
- Embedded frontend and OpenAPI 3.1 specification
- Non-root Docker image and Render Blueprint

## Architecture

```text
Browser / API client
        |
        v
Go HTTP server
  -> API key + URL/session validation + per-client limit
  -> session-fingerprinted in-memory cache
  -> singleflight + concurrency semaphore
  -> rate-limited LinkedIn Voyager client
  -> response normalizer
```

The service deliberately runs synchronously and stores neither profile data nor session cookies on disk. Cache keys contain a one-way session fingerprint, preventing data fetched with one account from being returned to another. One process is the intended deployment shape.

## Requirements

- Go 1.25 or Docker
- An authenticated LinkedIn session
- A long random API key for the hosted endpoint

## LinkedIn session configuration

The backend uses the same general session-cookie approach used by LinkedIn automation tools. It never needs the LinkedIn password.

1. Sign in to LinkedIn in a browser.
2. Open the browser developer tools.
3. Under **Application/Storage → Cookies → `https://www.linkedin.com`**, copy `li_at` (required) and `JSESSIONID` (recommended).
4. Paste them into the session fields in the frontend, or send them in the request body documented below.

`li_at` grants access equivalent to the signed-in session. Use only this HTTPS deployment or a local instance you trust. The frontend keeps values only in its current page fields; the server uses them for the request and does not write or return them. Never commit cookies or send them to an untrusted deployment.

## Local setup

```bash
cp .env.example .env
# Edit .env with your private values.
set -a
source .env
set +a
go run ./cmd/server
```

Open `http://localhost:8080`. Development mode permits an empty `APP_API_KEY`, but setting one locally is recommended. Production mode refuses to start without an API key.

Run with Docker:

```bash
docker build -t linkedin-profile-api .
docker run --rm -p 8080:8080 \
  -e APP_ENV=production \
  -e APP_API_KEY='replace-me' \
  linkedin-profile-api
```

## API

### Fetch a profile

`POST /api/v1/profiles`

```bash
curl --request POST 'http://localhost:8080/api/v1/profiles' \
  --header 'Content-Type: application/json' \
  --header 'X-API-Key: replace-me' \
  --data '{
    "url":"https://www.linkedin.com/in/example",
    "linkedin_session":{
      "li_at":"your-li-at-cookie",
      "jsession_id":"ajax:your-jsession-id"
    }
  }'
```

Example response:

```json
{
  "schema_version": "1.0",
  "profile": {
    "public_identifier": "example",
    "profile_url": "https://www.linkedin.com/in/example",
    "first_name": "Ada",
    "last_name": "Example",
    "full_name": "Ada Example",
    "headline": "Distributed Systems Engineer",
    "location": "London, United Kingdom",
    "about": "Builds reliable systems.",
    "profile_images": [],
    "experience": [],
    "education": [],
    "skills": [{"name": "Go"}],
    "certifications": [],
    "languages": [{"name": "English"}]
  },
  "meta": {
    "request_id": "5ca237b93cd2c9688ae8c40e",
    "fetched_at": "2026-08-31T16:30:00Z",
    "cache_hit": false,
    "partial": false,
    "warnings": []
  }
}
```

The complete contract is available at `/openapi.yaml` and in [`api/openapi.yaml`](api/openapi.yaml).

### Health

`GET /healthz` returns `{"status":"ok"}` without contacting LinkedIn or exposing session state.

### Errors

Errors have a stable envelope:

```json
{
  "error": {
    "code": "invalid_profile_url",
    "message": "invalid LinkedIn profile URL: expected /in/{public-identifier}",
    "request_id": "5ca237b93cd2c9688ae8c40e"
  }
}
```

The API uses `400` for invalid input, `401` for the evaluator API key, `403/404` for inaccessible profiles, `429` for local admission limits, `502` for unsupported upstream responses, `503` for LinkedIn session/cooldown failures, and `504` for timeouts.

## Configuration

- `APP_ENV` (`development`): set to `production` on the hosted service.
- `PORT` (`8080`): HTTP listen port; Render supplies this automatically.
- `APP_API_KEY` (empty): client API key; required in production.
- `LINKEDIN_DECORATION_ID` (`FullProfileWithEntities-93`): override when LinkedIn revises the Dash response decoration.
- `CACHE_TTL` (`30m`): in-memory profile cache lifetime.
- `CACHE_MAX_ENTRIES` (`200`): maximum cached profiles.
- `MAX_CONCURRENT_SCRAPES` (`2`): maximum distinct live profile requests.
- `RATE_LIMIT_RPM` (`10`): requests per client per minute.
- `RATE_LIMIT_BURST` (`3`): client token-bucket burst.
- `LINKEDIN_REQUEST_INTERVAL` (`2s`): minimum interval between LinkedIn calls.
- `REQUEST_TIMEOUT` (`25s`): end-to-end profile request deadline.
- `UPSTREAM_TIMEOUT` (`15s`): LinkedIn HTTP-client timeout.
- `LINKEDIN_COOLDOWN` (`15m`): pause after rate-limit or challenge responses.

Duration values use Go syntax such as `500ms`, `15s`, or `30m`.

## Deploy to Render

1. Push this repository to GitHub.
2. In Render, choose **New → Blueprint** and connect the repository.
3. Render reads [`render.yaml`](render.yaml) and creates a free Docker web service.
4. Enter a private `APP_API_KEY` when prompted.
5. Wait for `/healthz` to pass. Render provides an HTTPS `*.onrender.com` URL automatically.
6. Give evaluators the API URL and API key separately. Each caller provides their own LinkedIn session through the frontend.

Render's free instance may sleep while idle, so the first request can have a cold start. Its filesystem is ephemeral, which is acceptable because this service keeps only an in-memory cache. A stable outbound IP is not guaranteed and LinkedIn may occasionally ask the account to authenticate again.

## Tests

Tests use a local fake upstream and scrubbed profile fixtures. They never contact LinkedIn.

```bash
go test ./...
go test -race ./...
go vet ./...
```

The live integration is intentionally manual: provide valid session cookies in the frontend and request a profile that the account is allowed to view. Do not put live integration credentials into GitHub Actions.

## Approach

For each request, the service creates a short-lived client for the caller's `li_at` and `JSESSIONID`. It calls LinkedIn's authenticated Voyager Dash profile endpoint with a matching CSRF header and deterministic locale, resolves the normalized `included` entity graph, and then discards the client and cookies.

Only caller-provided LinkedIn `/in/{publicIdentifier}` URLs are accepted. The service extracts the identifier and constructs its own fixed upstream endpoint, so it never fetches an arbitrary caller-controlled URL.

LinkedIn calls are intentionally conservative. Network and selected `5xx` failures receive one bounded retry. Authentication failures, privacy denials, rate limits, and challenge pages are not retried.

## Known limitations

- Voyager is undocumented and its endpoint or response shape may change without notice.
- Returned data depends on what the caller's LinkedIn account can see, its locale, and LinkedIn experiments.
- Private, hidden, lazy-loaded, or unsupported sections may be absent and produce partial-result warnings.
- The current adapter targets the Dash `FullProfileWithEntities-93` response and retains compatibility with the older profile-view shape; future decoration versions may require a configuration or parser update.
- LinkedIn sessions expire and authentication challenges require manual operator action.
- CDN profile-image URLs can expire.
- Cache and rate limits reset when the Render instance restarts or sleeps.
- Session cookies travel through the server process for each request; callers must trust the deployment operator and HTTPS endpoint.
- One service instance is intended. This is not a high-throughput scraper or persistent session vault.
- The project does not create accounts, rotate identities, solve CAPTCHA, or bypass LinkedIn access controls.
