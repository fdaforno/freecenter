import { DatePipe } from '@angular/common';
import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ApiService, AuthService, DialogService, Role, User, errorMessage } from '../../core';

const MIN_LENGTH = 8;

interface UserForm {
  /** Set when editing an existing user. */
  user?: User;
  username: string;
  password: string;
  role: Role;
}

/** Administration > Users and Roles. */
@Component({
  selector: 'app-users-page',
  imports: [ClarityModule, FormsModule, DatePipe],
  template: `
    <div class="fc-page">
      <header>
        <cds-icon shape="users" size="24"></cds-icon>
        <span class="name">Users and Roles</span>
      </header>
      <div class="fc-page-body">
        @if (error()) {
          <clr-alert clrAlertType="danger" [clrAlertClosable]="true" (clrAlertClosedChange)="error.set('')">
            <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
          </clr-alert>
        }
        <div class="fc-toolbar">
          <button class="btn btn-sm btn-outline" (click)="openAdd()">Add User...</button>
        </div>
        <table class="table table-compact">
          <thead>
            <tr>
              <th class="left">User name</th>
              <th class="left">Role</th>
              <th class="left">Created</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            @for (u of users(); track u.id) {
              <tr>
                <td class="left">{{ u.username }}{{ u.username === auth.username() ? ' (you)' : '' }}</td>
                <td class="left">{{ u.role === 'admin' ? 'Administrator' : 'Read-only' }}</td>
                <td class="left">{{ u.created_at | date: 'MM/dd/yyyy, h:mm a' }}</td>
                <td class="right">
                  <button class="btn btn-sm btn-link" (click)="openEdit(u)">Edit...</button>
                  <button class="btn btn-sm btn-link" [disabled]="u.username === auth.username()" (click)="remove(u)">Delete</button>
                </td>
              </tr>
            }
          </tbody>
        </table>
        <p class="fc-muted">
          Administrators can do everything. Read-only users can look at the whole inventory, performance and events, but cannot change anything or open a
          virtual machine console.
        </p>
      </div>
    </div>

    @if (form(); as f) {
      <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="form.set(null)" [clrModalStaticBackdrop]="true" clrModalSize="md">
        <h3 class="modal-title">{{ f.user ? 'Edit User' : 'Add User' }} @if (f.user) {<span class="fc-muted">| {{ f.user.username }}</span>}</h3>
        <div class="modal-body">
          @if (formError()) {
            <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
              <clr-alert-item><span class="alert-text">{{ formError() }}</span></clr-alert-item>
            </clr-alert>
          }
          <form clrForm clrLayout="horizontal" (ngSubmit)="save()">
            @if (!f.user) {
              <clr-input-container>
                <label>User name</label>
                <input clrInput name="username" [(ngModel)]="f.username" autocomplete="off" maxlength="64" />
              </clr-input-container>
            }
            <clr-select-container>
              <label>Role</label>
              <select clrSelect name="role" [(ngModel)]="f.role">
                <option value="admin">Administrator</option>
                <option value="readonly">Read-only</option>
              </select>
            </clr-select-container>
            <clr-password-container>
              <label>{{ f.user ? 'New password' : 'Password' }}</label>
              <input clrPassword name="password" [(ngModel)]="f.password" autocomplete="new-password" />
              <clr-control-helper>{{ f.user ? 'Leave empty to keep the current one. ' : '' }}At least {{ minLength }} characters</clr-control-helper>
            </clr-password-container>
          </form>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" (click)="form.set(null)">Cancel</button>
          <button type="button" class="btn btn-primary" [clrLoading]="busy()" [disabled]="!valid(f)" (click)="save()">OK</button>
        </div>
      </clr-modal>
    }
  `,
  styles: `
    header {
      display: flex;
      align-items: center;
      gap: 0.4rem;
      padding: 0.5rem 0.9rem;
      background: #fff;
      border-bottom: 1px solid var(--fc-border);
    }
    .name {
      font-size: 1rem;
    }
    .table {
      margin-top: 0;
      max-width: 50rem;
    }
    .right {
      text-align: right;
      white-space: nowrap;
    }
    td .btn {
      margin: 0;
    }
  `,
})
export class UsersPage implements OnInit {
  private readonly api = inject(ApiService);
  private readonly dialogs = inject(DialogService);
  protected readonly auth = inject(AuthService);
  protected readonly minLength = MIN_LENGTH;

  protected readonly users = signal<User[]>([]);
  protected readonly error = signal('');
  protected readonly form = signal<UserForm | null>(null);
  protected readonly formError = signal('');
  protected readonly busy = signal(false);

  ngOnInit(): void {
    void this.load();
  }

  private async load(): Promise<void> {
    try {
      this.users.set(await this.api.users());
    } catch (e) {
      this.error.set(errorMessage(e));
    }
  }

  protected openAdd(): void {
    this.formError.set('');
    this.form.set({ username: '', password: '', role: 'readonly' });
  }

  protected openEdit(user: User): void {
    this.formError.set('');
    this.form.set({ user, username: user.username, password: '', role: user.role });
  }

  protected valid(f: UserForm): boolean {
    if (f.user) return f.password === '' || f.password.length >= MIN_LENGTH;
    return /^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$/.test(f.username.trim()) && f.password.length >= MIN_LENGTH;
  }

  protected async save(): Promise<void> {
    const f = this.form();
    if (!f || !this.valid(f) || this.busy()) return;
    this.formError.set('');
    this.busy.set(true);
    try {
      if (f.user) {
        await this.api.updateUser(f.user.id, { ...(f.role !== f.user.role ? { role: f.role } : {}), ...(f.password ? { password: f.password } : {}) });
      } else {
        await this.api.createUser(f.username.trim(), f.password, f.role);
      }
      this.form.set(null);
      await this.load();
    } catch (e) {
      this.formError.set(errorMessage(e));
    } finally {
      this.busy.set(false);
    }
  }

  protected async remove(user: User): Promise<void> {
    const ok = await this.dialogs.confirm({ title: 'Delete User', message: `Delete the user "${user.username}"? Their sessions end immediately.`, okLabel: 'Delete', danger: true });
    if (!ok) return;
    try {
      await this.api.deleteUser(user.id);
      await this.load();
    } catch (e) {
      this.error.set(errorMessage(e));
    }
  }
}
