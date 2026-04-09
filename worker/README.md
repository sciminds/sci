# sci-auth Worker

Cloudflare Worker that brokers GitHub OAuth device flow for the `sci` CLI. Verifies `sciminds` GitHub org membership and returns shared R2 credentials.

## Auth Flow

```
sci CLI                     Cloudflare Worker               GitHub API
  │                              │                              │
  ├─ POST /auth/device ─────────>│─ POST /login/device/code ───>│
  │<── {user_code, url} ─────────│<── {device_code, ...} ───────│
  │                              │                              │
  │  (user visits github.com/login/device, enters code)         │
  │                              │                              │
  ├─ POST /auth/token ──────────>│─ POST /login/oauth/token ───>│
  │   (polls every 5s)           │<── access_token ─────────────│
  │                              │─ GET /user ──────────────────>│
  │                              │─ GET /user/orgs ─────────────>│
  │                              │  (verify sciminds membership) │
  │<── R2 credentials ───────────│                              │
```

## Endpoints

**`POST /auth/device`** — Initiates the device flow. Returns `{device_code, user_code, verification_uri, expires_in, interval}`.

**`POST /auth/token`** — Accepts `{device_code}`. Polls GitHub for the access token, verifies org membership, and returns R2 bucket credentials on success. Returns `{status: "pending"}` while waiting for user approval.

## Deploy

```bash
bun install
bunx wrangler deploy
```

## Secrets

Set via `bunx wrangler secret put <NAME>`:

| Secret | Description |
|--------|-------------|
| `GITHUB_CLIENT_SECRET` | OAuth App client secret |
| `R2_ACCOUNT_ID` | Cloudflare account ID |
| `R2_ACCESS_KEY` | Public bucket access key |
| `R2_SECRET_KEY` | Public bucket secret key |
| `R2_PUBLIC_URL` | Public bucket URL (e.g. `https://pub-xxx.r2.dev`) |

The GitHub OAuth App Client ID and org name are configured as plain vars in `wrangler.toml`.
