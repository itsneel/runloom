# Local setup reference

Start a local installation from the repository root:

```sh
make local-services-up SERVICES_DATABASE=mongodb
```

Use `SERVICES_DATABASE=postgres` for a separate PostgreSQL installation. Docker with Compose, Make and supported Node.js must be installed.

## Sign in

Use the connection/account table printed by setup. Studio normally uses port **34200** for MongoDB or **35200** for PostgreSQL.

Organization admin/member accounts build workflows. Separate platform accounts use the platform console. Repeated setup preserves existing passwords; printed defaults may not match a password you changed.

## Stop safely

```sh
make local-services-down SERVICES_DATABASE=mongodb
```

This preserves volumes and credentials. Do not delete vault keys or databases to fix login or startup problems. These default accounts are for local use only.

Expand the reference for account defaults, reset commands and configuration paths.
