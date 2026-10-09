import { Component, inject, signal, OnInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { ApiError, ApiService, AuthService, errorMessage } from '../../core';

@Component({
  selector: 'app-login-page',
  imports: [ClarityModule, FormsModule],
  template: `
    <div class="login-wrapper">
      <form class="login" (ngSubmit)="submit()">
        <section class="title">
          <h3 class="welcome">Welcome to</h3>
          FreeCenter
          <h5 class="hint">Use your FreeCenter credentials to sign in</h5>
        </section>
        <div class="login-group">
          <clr-input-container>
            <label class="clr-sr-only">Username</label>
            <input type="text" name="username" clrInput placeholder="Username" autocomplete="username" [(ngModel)]="username" />
          </clr-input-container>
          <clr-password-container>
            <label class="clr-sr-only">Password</label>
            <input type="password" name="password" clrPassword placeholder="Password" autocomplete="current-password" [(ngModel)]="password" />
          </clr-password-container>
          @if (error()) {
            <div class="error active">{{ error() }}</div>
          }
          <button type="submit" class="btn btn-primary" [disabled]="busy()">{{ busy() ? 'Signing in...' : 'Login' }}</button>
          @if (oidcEnabled()) {
            <div class="oidc-divider"><span>or</span></div>
            <button type="button" class="btn btn-outline" (click)="oidcLogin()">
              <cds-icon shape="shield-check" size="16"></cds-icon>
              Sign in with OIDC
            </button>
          }
        </div>
      </form>
    </div>
  `,
  styles: `
    .login-wrapper {
      background: linear-gradient(135deg, #1b2a32 0%, #25404f 45%, #0f5e85 100%);
    }
    .oidc-divider {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      margin: 0.8rem 0 0.5rem;
      color: #ccc;
      font-size: 0.7rem;
    }
    .oidc-divider::before,
    .oidc-divider::after {
      content: '';
      flex: 1;
      height: 1px;
      background: rgba(255, 255, 255, 0.2);
    }
    .btn-outline {
      width: 100%;
      color: #fff;
      border-color: rgba(255, 255, 255, 0.3);
      background: transparent;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 0.4rem;
    }
    .btn-outline:hover {
      background: rgba(255, 255, 255, 0.1);
    }
  `,
})
export class LoginPage implements OnInit {
  private readonly api = inject(ApiService);
  private readonly router = inject(Router);
  private readonly auth = inject(AuthService);

  protected username = '';
  protected password = '';
  protected readonly error = signal('');
  protected readonly busy = signal(false);
  protected readonly oidcEnabled = signal(false);

  constructor() {
    if (this.auth.authenticated()) void this.router.navigate(['/']);
  }

  ngOnInit(): void {
    this.api.oidcConfig().then((cfg) => this.oidcEnabled.set(cfg.enabled)).catch(() => { /* OIDC not configured */ });
  }

  protected oidcLogin(): void {
    window.location.href = '/auth/oidc/login';
  }

  protected async submit(): Promise<void> {
    this.error.set('');
    this.busy.set(true);
    try {
      await this.api.login(this.username, this.password);
      await this.router.navigate(['/']);
    } catch (e) {
      this.error.set(e instanceof ApiError && e.status === 401 ? 'Invalid user name or password' : errorMessage(e));
    } finally {
      this.busy.set(false);
    }
  }
}
