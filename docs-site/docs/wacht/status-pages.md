---
slug: /status-pages
---

# Status Pages

Wacht has an authenticated status API and one anonymous public status page per
user.

## Authenticated Status

`GET /status` returns checks and probes for the authenticated user.

Example:

```sh
TOKEN=$(curl -s -X POST http://localhost:3000/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@wacht.local","password":"changeme"}' | jq -r .token)

curl -H "Authorization: Bearer $TOKEN" http://localhost:3000/status
```

The authenticated response includes:

- check ID
- check name
- target
- aggregate status
- incident start time when an incident is open
- probe ID
- probe status
- probe last-seen timestamp

## Public Status Page

Each user gets one public page:

```text
/public/{slug}
```

The dashboard shows the share URL on the Account page.

The backing JSON endpoint is:

```text
GET /api/public/status/{slug}
```

The public response includes:

- check ID
- check name
- aggregate status
- incident start time when an incident is open

The public response does not include:

- raw targets
- webhook URLs
- probe details
- incident history
- account email

## Current Limitations

- Public status pages cannot be disabled yet.
- Public status slugs cannot be rotated yet.
- There is one public status page per user.
- Public pages show current status only, not full incident history.
