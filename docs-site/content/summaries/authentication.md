# Teams & access

Keycloak handles sign-in. Runloom decides which organizations, teams and workflows an account can access.

## Choose the right access

| Account | Intended use |
| --- | --- |
| Organization/team member | Work within assigned teams and roles |
| Organization/team administrator | Manage authorized organization or team resources |
| Service account | Call granted consumer execution operations |
| Platform administrator/staff | Use the separate platform console |

Platform access does not automatically grant access to tenant workflows. Service-account tokens cannot be used on Studio management endpoints.

## Sign in

Open your installation's Studio URL and use interactive login. Google sign-in appears only when configured by the operator. Password recovery is handled through Keycloak.

If access is denied, check the selected organization, team and assigned role before changing credentials. See [API interfaces](/api/overview) for integration access.
