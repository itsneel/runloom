# Platform API

Platform operations use `/api/v1/platform`. A human Studio access token and a Runloom platform assignment are required. Organization administrator access alone is not sufficient, and service-account credentials are not accepted.

## Permissions

Platform staff may view platform identity, service health and organization metadata. Platform administrators may additionally manage staff access and read platform audit events. These permissions do not grant tenant workflow access.

Hosted installations also enforce configured authentication context and session age requirements. See [Platform administration](/management/platform) for setup and limitations.

## Interactive testing

The contract below is read-only. Open the installed Swagger interface through the API reference controls to authenticate and test against your selected installation. Do not paste tokens or credentials into documentation search or shared URLs.
