import { bootstrapApplication } from '@angular/platform-browser';
import {
  loadCoreIconSet,
  loadEssentialIconSet,
  loadMediaIconSet,
  loadTechnologyIconSet,
} from '@clr/angular/icon';

import { App } from './app/app';
import { appConfig } from './app/app.config';

loadCoreIconSet();
loadEssentialIconSet();
loadTechnologyIconSet();
loadMediaIconSet();

bootstrapApplication(App, appConfig).catch((err) => console.error(err));
