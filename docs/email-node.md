# Email node

Add **Email** in Studio, enter To, optional CC/BCC/Reply-To, subject, a required plain-text body and an optional HTML alternative. Recipient fields accept commas or newlines. Fields support normal `${input...}` and `${steps...}` interpolation. The worker sends through operator-configured SMTP; credentials and sender identity are not editable in workflow JSON.

Try [email-notification.json](../examples/email-notification.json) with `{"email":"recipient@example.test","name":"Runloom"}`. The default development Compose/Helm setup delivers to Mailpit, not the public internet. Open local Mailpit at http://localhost:8025. Native workers need SMTP environment configuration explicitly.

## Delivery rules

1. Sending happens in a Temporal Activity, never in replayable workflow code. Email has one attempt, a 30-second activity execution timeout and a two-minute scheduling/execution window. Workflow authors cannot override these with retry or timeout fields. A manual rerun can send another email.
2. Success means SMTP accepted the message, not that an inbox received it. SMTP cannot guarantee exactly-once delivery: a lost acknowledgement or worker crash can leave delivery uncertain. Check the provider before manually retrying. All recipient commands must succeed before the body is transmitted.
3. Output is `{accepted: true, message_id: "...", recipient_count: N}`. Provider errors are sanitized. BCC is omitted from the transmitted message headers but remains part of workflow definitions/activity inputs and may be visible in authorized run history. It is not a secret-storage mechanism.
4. Addresses are plain ASCII mailboxes, not display-name lists. Validation runs both before publishing and after interpolation. Header injection is rejected. Limit: 50 To/CC/BCC entries combined, 300 UTF-8 bytes in the subject and 256 KiB for text plus HTML combined.
5. HTML values are not automatically escaped. Avoid interpolating untrusted markup; use a plain-text email or prepare sanitized HTML. The browser never previews arbitrary HTML as trusted markup.

## Worker configuration

Set `RUNLOOM_SMTP_HOST`, `RUNLOOM_SMTP_PORT` (default 587, or 465 for implicit TLS), `RUNLOOM_SMTP_FROM` and `RUNLOOM_SMTP_TLS` (`starttls`, `tls`, or `none` for trusted local Mailpit only). STARTTLS is the default and requires server support with no plaintext fallback. TLS certificates and hostname are verified; TLS 1.2 is the minimum.

For authenticated SMTP, also set `RUNLOOM_SMTP_USERNAME` and `RUNLOOM_SMTP_PASSWORD_FILE`. The password is read from a file, never from workflow JSON or a password environment value. Plaintext authentication is rejected. Optional `RUNLOOM_SMTP_ALLOWED_DOMAINS` is a comma-separated exact recipient-domain allowlist; it applies to To, CC and BCC. An empty host disables delivery and Email nodes fail with a configuration error.

Compose's base worker uses local Mailpit. For external SMTP, add `-f compose/compose.email.yaml` to your usual Compose files and select a configuration file with `RUNLOOM_EMAIL_CONFIG_PATH`. Copy `compose/config/email.example.yaml` outside the repository and set the hostname, verified sender, username and optional allowed domains in that file. Supply an absolute `RUNLOOM_SMTP_PASSWORD_PATH` separately. The overlay mounts the file only into the worker, defaults to STARTTLS/587, and also accepts the domain allowlist. Store the password outside source control and ensure the worker UID can read the mount. Preserve any pricing/secrets overlays you already use.

Helm uses `email.enabled`, `email.host`, `email.port`, `email.from`, `email.tls`, `email.username`, `email.existingSecret`, `email.secretKey` and `email.allowedDomains`. An empty host uses bundled Mailpit only when development services are enabled. External SMTP requires TLS. The production example disables Email until an operator configures it; a username requires an existing password Secret. Only the worker receives that Secret mount. SMTP credentials are shared operator configuration in this release, not per-team accounts.

Run `make test-email` for MIME, BCC, address/header validation, STARTTLS enforcement, recipient rejection, cancellation and single-attempt workflow tests. Browser coverage is included in `make test-ui-costs-email`.

With the default local Compose stack and admin account running, `node scripts/email-live-smoke.mjs` signs in through Keycloak, publishes a unique sample, starts it through Temporal and verifies the message in Mailpit plus its stored run history and Studio cost panel. This retains one workflow/run/message for inspection. Run only against the default Mailpit worker configuration, never external SMTP. It uses the same Playwright/Chrome environment options as other browser smoke scripts.
