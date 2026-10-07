import { Component, effect, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { DialogService } from '../core';

/** Renders DialogService.confirm()/prompt() requests. */
@Component({
  selector: 'app-confirm-dialog',
  imports: [ClarityModule, FormsModule],
  template: `
    @if (dialogs.confirmState(); as s) {
      <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="s.resolve(null)" clrModalSize="md">
        <h3 class="modal-title">{{ s.title }}</h3>
        <div class="modal-body">
          <p>{{ s.message }}</p>
          @if (s.promptLabel) {
            <form clrForm clrLayout="vertical" (ngSubmit)="s.resolve(text())">
              <clr-input-container>
                <label>{{ s.promptLabel }}</label>
                <input clrInput name="text" [ngModel]="text()" (ngModelChange)="text.set($event)" size="50" />
              </clr-input-container>
            </form>
          }
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" (click)="s.resolve(null)">Cancel</button>
          <button type="button" class="btn" [class.btn-danger]="s.danger" [class.btn-primary]="!s.danger" (click)="s.resolve(text())">
            {{ s.okLabel || 'OK' }}
          </button>
        </div>
      </clr-modal>
    }
  `,
})
export class ConfirmDialogComponent {
  protected readonly dialogs = inject(DialogService);
  protected readonly text = signal('');

  constructor() {
    effect(() => this.text.set(this.dialogs.confirmState()?.promptDefault ?? ''));
  }
}
