'use strict';
(() => {
  const studio = location.pathname === '/api/docs/studio/';
  const platform = location.pathname === '/api/docs/platform/';
  const name = platform ? 'Platform' : studio ? 'Studio' : 'Consumer';
  const spec = platform ? '/api/v1/platform/openapi.json' : studio ? '/api/v1/studio/openapi.json' : '/api/v1/openapi.json';
  const prefix = platform ? '/api/v1/platform/' : studio ? '/api/v1/studio/' : '/api/v1/';
  document.title = `Runloom ${name} API`;
  document.getElementById('docs-title').textContent = `${name} API reference`;
  document.getElementById('docs-description').textContent = platform ? 'Verified human token and separate platform permissions required. Staff can read operational status and organization metadata only. Hosted deployments require recent MFA. No tenant permission bypass.' : studio
    ? 'Human Studio access token required. Team-scoped operations also require X-Runloom-Team. Viewers can read; editors/admins can operate runs. Management has additional role checks.'
    : 'Service-account execution token required. Scopes, exact published workflow/version grants and ownership restrict access. Pause/stop/resume are not consumer operations; use cancellation for permanent stopping.';
  document.querySelector(`[data-interface="${platform ? 'platform' : studio ? 'studio' : 'consumer'}"]`).setAttribute('aria-current', 'page');
  document.getElementById('download-spec').href = spec;
  window.ui = SwaggerUIBundle({
    url: spec,
    dom_id: '#swagger-ui',
    deepLinking: true,
    filter: true,
    docExpansion: 'none',
    defaultModelsExpandDepth: -1,
    displayRequestDuration: true,
    persistAuthorization: false,
    validatorUrl: null,
    queryConfigEnabled: false,
    withCredentials: false,
    supportedSubmitMethods: ['get', 'post', 'put', 'delete'],
    requestInterceptor(request) {
      const target = new URL(request.url, location.origin);
      if (target.origin !== location.origin || !target.pathname.startsWith(prefix) || (!studio && !platform && (target.pathname.startsWith('/api/v1/studio/') || target.pathname.startsWith('/api/v1/platform/')))) {
        throw new Error('This documentation page only sends requests to its own API interface and origin.');
      }
      request.credentials = 'omit';
      return request;
    },
  });
})();
