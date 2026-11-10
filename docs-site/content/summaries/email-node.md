# Email

Send email from a workflow using the SMTP service configured by your operator.

## Send an email

1. Add an **Email** node.
2. Set recipients, subject and plain-text or HTML content.
3. Map workflow input into supported fields.
4. Test with a safe recipient and inspect delivery.

Local development delivers to Mailpit. Hosted delivery needs an operator-configured mail server; it is not enabled by simply adding a node.

## Before testing

Tests can send real email. Keep SMTP credentials in server configuration, never in the workflow. Review retry and delivery rules before using email for irreversible business notifications.
