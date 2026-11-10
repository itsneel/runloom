# External APIs & secrets

Call an external service without storing its credentials in workflow JSON.

## Make a request

1. Save the credential in **Management → Workflow secrets**.
2. Add an **External API** node.
3. Configure the method, URL and request data. Reference the saved secret for authentication.
4. Check the response mapping and test against a safe endpoint.

Secret names start with a lowercase letter and use lowercase letters, digits and hyphens. Saved values cannot be retrieved from the UI.

## Keep requests safe

Tests send real requests. Use idempotency keys where the remote API supports them, especially for writes. Destination restrictions apply; do not assume every host is reachable.

Operators must preserve the vault encryption key across restarts and back it up separately from the database. Losing it makes existing secrets unreadable.
