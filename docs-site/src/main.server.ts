import { mergeApplicationConfig } from '@angular/core';
import { bootstrapApplication, BootstrapContext } from '@angular/platform-browser';
import { provideServerRendering, withRoutes, RenderMode } from '@angular/ssr';
import { ShellComponent } from './app/layout/shell/shell.component';
import { appConfig } from './app/core/app.config';

export default (context: BootstrapContext) =>
  bootstrapApplication(
    ShellComponent,
    mergeApplicationConfig(appConfig, {
      providers: [
        provideServerRendering(withRoutes([{ path: '**', renderMode: RenderMode.Prerender }])),
      ],
    }),
    context,
  );
