import { Component, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ApiService, AuthService, DialogService, errorMessage } from '../../core';

const MIN_LENGTH = 8;

/** Change the password of the signed-in user. */
@Component({
  selector: 'app-change-password-dialog',
  imports: [ClarityModule, FormsModule],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="md">
      <h3 class="modal-title">Change Password <span class="fc-muted">| {{ auth.username() }}</span></h3>
      <div class="modal-body">
        @if (error()) {
          <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
            <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
          </clr-alert>
        }
        <form clrForm clrLayout="horizontal" (ngSubmit)="save()">
          <clr-password-container>
            <label>Current password</label>
            <input clrPassword name="current" [(ngModel)]="current" autocomplete="current-password" />
          </clr-password-container>
          <clr-password-container>
            <label>New password</label>
            <input clrPassword name="next" [(ngModel)]="next" autocomplete="new-password" />
            <clr-control-helper>At least {{ minLength }} characters</clr-control-helper>
          </clr-password-container>
          <clr-password-container>
            <label>Confirm new password</label>
            <input clrPassword name="confirm" [(ngModel)]="confirm" autocomplete="new-password" />
            @if (confirm && confirm !== next) {
              <clr-control-helper>The two passwords do not match</clr-control-helper>
            }
          </clr-password-container>
        </form>
        <p class="fc-muted">Your other sessions are signed out; this one stays open.</p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" (click)="close()">Cancel</button>
        <button type="button" class="btn btn-primary" [clrLoading]="busy()" [disabled]="!valid()" (click)="save()">OK</button>
      </div>
    </clr-modal>
  `,
})
export class ChangePasswordDialogComponent {
  private readonly api = inject(ApiService);
  private readonly dialogs = inject(DialogService);
  protected readonly auth = inject(AuthService);
  protected readonly minLength = MIN_LENGTH;

  protected current = '';
  protected next = '';
  protected confirm = '';
  protected readonly error = signal('');
  protected readonly busy = signal(false);

  protected valid(): boolean {
    return !!this.current && this.next.length >= MIN_LENGTH && this.next === this.confirm;
  }

  protected close(): void {
    this.dialogs.changePassword.set(false);
  }

  protected async save(): Promise<void> {
    if (!this.valid() || this.busy()) return;
    this.error.set('');
    this.busy.set(true);
    try {
      await this.api.changePassword(this.current, this.next);
      this.close();
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.busy.set(false);
    }
  }
}
