import { bootstrapApplication } from '@angular/platform-browser';
import { appConfig } from './app.config';
import { Shell } from './layout/shell/shell.component';

bootstrapApplication(Shell, appConfig)
  .catch((err) => console.error(err));
