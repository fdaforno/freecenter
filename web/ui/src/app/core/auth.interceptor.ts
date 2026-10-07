import { HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';

import { AuthService } from './auth.service';
import { TaskContext } from './task.service';

const READ_METHODS = ['GET', 'HEAD', 'OPTIONS'];

/** Adds the session token, and the task name to the request that starts an operation. */
export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const headers: Record<string, string> = {};
  const token = inject(AuthService).token();
  if (token) headers['Authorization'] = `Bearer ${token}`;
  if (!READ_METHODS.includes(req.method)) {
    const task = inject(TaskContext).take();
    if (task) {
      // Header values must be ASCII: names can contain anything.
      headers['X-Task-Name'] = encodeURIComponent(task.name);
      headers['X-Task-Target'] = encodeURIComponent(task.target);
    }
  }
  return next(req.clone({ setHeaders: headers }));
};
