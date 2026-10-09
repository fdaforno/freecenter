import { Component, inject, signal } from '@angular/core';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { AuthService } from '../../core';

/** Receives the local session token after a successful OIDC redirect. */
@Component({
  selector: 'app-oidc-callback',
  imports: [RouterLink],
  template: `
    @if (error()) {
      <div class="fc-empty">
        <p>{{ error() }}</p>
        <a routerLink="/login">Back to login</a>
      </div>
    } @else {
      <div class="fc-empty"><p>Signing in…</p></div>
    }
  `,
  styles: [`
    :host { display: flex; align-items: center; justify-content: center; height: 100vh; }
    a { color: #0072a3; }
  `],
})
export class OidcCallbackPage {
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly auth = inject(AuthService);

  private readonly errSig = signal('');

  protected error() { return this.errSig(); }

  constructor() {
    const params = this.route.snapshot.queryParamMap;
    const token = params.get('token');
    const username = params.get('username') ?? '';
    const role = params.get('role') ?? 'readonly';
    const err = params.get('error');

    if (err) {
      this.errSig.set(err);
      return;
    }
    if (token) {
      this.auth.setSession(token, username, role, false);
      void this.router.navigate(['/']);
      return;
    }
    this.errSig.set('No session token received from the identity provider.');
  }
}
