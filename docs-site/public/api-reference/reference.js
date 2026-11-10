const candidate = new URLSearchParams(location.search).get('api');
const api = ['studio', 'consumer', 'platform'].includes(candidate) ? candidate : 'consumer';
fetch(`/generated/contracts/${api}.json`)
  .then((response) => {
    if (!response.ok) throw new Error('Contract unavailable');
    return response.json();
  })
  .then((spec) => {
    // Public reference never accepts credentials or sends API operations.
    delete spec.security;
    if (spec.components) delete spec.components.securitySchemes;
    for (const path of Object.values(spec.paths || {})) {
      for (const operation of Object.values(path)) {
        if (operation && typeof operation === 'object') delete operation.security;
      }
    }
    SwaggerUIBundle({
      spec,
      dom_id: '#swagger-ui',
      validatorUrl: null,
      supportedSubmitMethods: [],
      persistAuthorization: false,
      tryItOutEnabled: false,
      defaultModelsExpandDepth: -1,
      docExpansion: 'list',
      filter: true,
      displayRequestDuration: false,
      deepLinking: false,
    });
  })
  .catch(() => {
    document.getElementById('swagger-ui').textContent =
      'API reference could not load. Reload this page or download the OpenAPI contract.';
  });
