import { ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { authInterceptor } from '@runloom/studio-platform';
import { provideRouter, withDisabledInitialNavigation, withRouterConfig } from '@angular/router';
import { routes } from './routing/app.routes';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideHttpClient(withInterceptors([authInterceptor])),
    // Process the OIDC callback before redirects can rewrite its URL/query string.
    provideRouter(routes, withDisabledInitialNavigation(), withRouterConfig({ canceledNavigationResolution: 'computed' }))
  ]
};
