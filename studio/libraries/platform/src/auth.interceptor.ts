import { inject } from '@angular/core';
import { HttpInterceptorFn } from '@angular/common/http';
import { from, switchMap } from 'rxjs';
import { AuthService } from './auth.service';

export const authInterceptor: HttpInterceptorFn = (request, next) => {
  if (!request.url.startsWith('/api/v1/studio/')) return next(request);
  const auth = inject(AuthService);
  const team = request.headers.get('X-Runloom-Team') || auth.teamId();
  return from(auth.bearer()).pipe(
    switchMap((token) =>
      next(
        request.clone({
          setHeaders: { Authorization: 'Bearer ' + token, 'X-Runloom-Team': team },
        }),
      ),
    ),
  );
};
