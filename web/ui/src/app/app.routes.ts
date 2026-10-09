import { Routes } from '@angular/router';

import { adminGuard, authGuard } from './core/auth.service';

export const routes: Routes = [
  { path: 'login', loadComponent: () => import('./features/login/login.page').then((m) => m.LoginPage) },
  { path: 'auth/oidc/callback', loadComponent: () => import('./features/login/oidc-callback.page').then((m) => m.OidcCallbackPage) },
  {
    // Full-page VM console, opened in its own tab (no shell around it).
    path: 'console/:hid/:vmid',
    canActivate: [authGuard],
    loadComponent: () => import('./features/console/console.page').then((m) => m.ConsolePage),
  },
  {
    path: '',
    canActivate: [authGuard],
    loadComponent: () => import('./shell/shell.component').then((m) => m.ShellComponent),
    children: [
      { path: '', pathMatch: 'full', redirectTo: 'root/summary' },
      { path: 'root', pathMatch: 'full', redirectTo: 'root/summary' },
      { path: 'admin/users', canActivate: [adminGuard], loadComponent: () => import('./features/admin/users.page').then((m) => m.UsersPage) },
      { path: 'admin/auth', canActivate: [adminGuard], loadComponent: () => import('./features/admin/auth.page').then((m) => m.AuthPage) },
      { path: 'root/:tab', loadComponent: () => import('./features/root/root.page').then((m) => m.RootPage) },
      { path: 'host/:hid', pathMatch: 'full', redirectTo: 'host/:hid/summary' },
      { path: 'host/:hid/:tab', loadComponent: () => import('./features/host/host.page').then((m) => m.HostPage) },
      { path: 'vm/:hid/:vmid', pathMatch: 'full', redirectTo: 'vm/:hid/:vmid/summary' },
      { path: 'vm/:hid/:vmid/:tab', loadComponent: () => import('./features/vm/vm.page').then((m) => m.VmPage) },
      { path: 'datastore/:hid/:name', pathMatch: 'full', redirectTo: 'datastore/:hid/:name/summary' },
      {
        path: 'datastore/:hid/:name/:tab',
        loadComponent: () => import('./features/datastore/datastore.page').then((m) => m.DatastorePage),
      },
      { path: 'network/:hid/:name', pathMatch: 'full', redirectTo: 'network/:hid/:name/summary' },
      {
        path: 'network/:hid/:name/:tab',
        loadComponent: () => import('./features/network/network.page').then((m) => m.NetworkPage),
      },
    ],
  },
  { path: '**', redirectTo: '' },
];
