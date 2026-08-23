# M5.5A managed surfaces handoff

The M5.5A dogfood branch adds role-scoped management surfaces without
changing the frozen Node → Hub snapshot contract.

- Node-only `GET /settings` renders the loopback-local configuration form.
- Node-only `POST /settings` requires the rendered CSRF token, preserves a
  stored node token when the password field is blank, atomically saves the
  complete config, and requests a graceful supervised restart.
- Hub-only `GET /admin` renders either the login form or authenticated node
  management. Normal reads never contain node or admin tokens.
- `POST /admin/login` verifies the secret from `admin.token_file` and creates
  a signed, HttpOnly, SameSite=Strict, bounded session cookie.
- Authenticated Hub node mutations live under `/admin/nodes/*`, require the
  session-bound CSRF token, atomically save the registry, and request restart.
  Add/reset display a newly generated node token once in that response.
- `devboard healthcheck --url URL [--expect-role node|hub]` performs a
  bounded GET and requires a valid healthy role response.

The existing `/api/node/v1/snapshot`, `/api/dashboard`, and `/display`
interfaces and their push-only authority remain unchanged.
