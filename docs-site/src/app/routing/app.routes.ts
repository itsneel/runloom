import { Routes } from '@angular/router';
import { articleRoutes } from '../core/content/generated/routes';

export const routes: Routes = [
  {
    path: '',
    title: 'Runloom documentation',
    loadComponent: () => import('../features/home/home.component').then((m) => m.HomeComponent),
  },
  ...articleRoutes,
  {
    path: '**',
    title: 'Page not found · Runloom',
    loadComponent: () =>
      import('../features/not-found/not-found.component').then((m) => m.NotFoundComponent),
  },
];
