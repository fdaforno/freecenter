import { Component, inject, signal } from '@angular/core';
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
        </div>
      </form>
    </div>
  `,
  styles: `
    .login-wrapper {
      background: linear-gradient(135deg, #1b2a32 0%, #25404f 45%, #0f5e85 100%);
    }
  `,
})
export class LoginPage {
  private readonly api = inject(ApiService);
  private readonly router = inject(Router);

  protected username = '';
  protected password = '';
  protected readonly error = signal('');
  protected readonly busy = signal(false);

  constructor() {
    if (inject(AuthService).authenticated()) void this.router.navigate(['/']);
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
