import { bootstrapApplication } from '@angular/platform-browser';
import { ShellComponent } from './app/layout/shell/shell.component';
import { appConfig } from './app/core/app.config';

bootstrapApplication(ShellComponent, appConfig).catch(console.error);
