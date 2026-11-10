# Application databases

Runloom supports MongoDB and PostgreSQL for application storage. This is separate from the databases your workflows connect to.

## What is stored?

Auth, Workflows, Management and Assistant each own their records and database credentials. Records include access, definitions, run snapshots, audit events, usage and conversations.

Temporal history—not the application database—is the authority for execution replay. Workflow code does not read the database directly.

## Choose and protect storage

1. Select MongoDB or PostgreSQL when setting up an installation.
2. Keep owner credentials separate and enforce database permissions.
3. Back up the databases and encryption keys separately.
4. Test recovery before relying on backups.

Switching drivers does not migrate data. Avoid deleting volumes or regenerating encryption keys during troubleshooting.

To query your own systems from a workflow, use [Database connections](/integrations/connections).
