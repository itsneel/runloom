# API overview

Choose the interface that matches your caller. All application APIs are versioned under `/api/v1`.

| Interface | Caller | Purpose |
| --- | --- | --- |
| [Consumer](/api/consumer) | Service account | List granted workflows; start, inspect and cancel owned runs |
| [Studio](/api/studio) | Signed-in person | Author workflows, manage team resources and operate runs |
| [Platform](/api/platform) | Platform admin/staff | Restricted platform operations |

## Try an API

1. Open the appropriate reference.
2. Choose your installation using **Studio connection**.
3. Use **Open installed Swagger** to authenticate and send requests there.

The embedded contracts are read-only. Real requests can start work or change data. Never embed client secrets in browser apps; use interactive login for people.

Consumer credentials cannot pause or resume runs and are rejected on Studio/management endpoints. Check scopes and grants—not just whether a token is valid.
