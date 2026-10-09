import { Injectable, computed, inject, signal } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';

const TOKEN_KEY = 'fc_token';
const USER_KEY = 'fc_user';
const ROLE_KEY = 'fc_role';
const MUST_CHANGE_KEY = 'fc_must_change_password';

@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly router = inject(Router);
  private readonly tokenSig = signal(localStorage.getItem(TOKEN_KEY) ?? '');
  private readonly userSig = signal(localStorage.getItem(USER_KEY) ?? '');
  private readonly roleSig = signal(localStorage.getItem(ROLE_KEY) ?? 'admin');
  private readonly mustChangeSig = signal(localStorage.getItem(MUST_CHANGE_KEY) === '1');

  readonly token = this.tokenSig.asReadonly();
  readonly username = this.userSig.asReadonly();
  readonly role = this.roleSig.asReadonly();
  readonly authenticated = computed(() => this.tokenSig() !== '');
  readonly isAdmin = computed(() => {
    const r = this.roleSig();
    return r === 'admin' || r === '';
  });
  /** The session was opened with the default password: the UI keeps asking to change it. */
  readonly mustChangePassword = this.mustChangeSig.asReadonly();

  setSession(token: string, username: string, role: string, mustChangePassword: boolean): void {
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(USER_KEY, username);
    localStorage.setItem(ROLE_KEY, role);
    this.tokenSig.set(token);
    this.userSig.set(username);
    this.roleSig.set(role);
    this.setMustChangePassword(mustChangePassword);
  }

  setMustChangePassword(value: boolean): void {
    if (value) localStorage.setItem(MUST_CHANGE_KEY, '1');
    else localStorage.removeItem(MUST_CHANGE_KEY);
    this.mustChangeSig.set(value);
  }

  clearSession(): void {
    for (const key of [TOKEN_KEY, USER_KEY, ROLE_KEY, MUST_CHANGE_KEY]) localStorage.removeItem(key);
    this.tokenSig.set('');
    this.userSig.set('');
    this.mustChangeSig.set(false);
    void this.router.navigate(['/login']);
  }
}

export const authGuard: CanActivateFn = () => {
  const auth = inject(AuthService);
  return auth.authenticated() ? true : inject(Router).createUrlTree(['/login']);
};

export const adminGuard: CanActivateFn = () => {
  const auth = inject(AuthService);
  return auth.isAdmin() ? true : inject(Router).createUrlTree(['/']);
};
