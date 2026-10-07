import { Component, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ApiService, DialogService, InventoryService, TaskService, Vm, errorMessage } from '../../core';

/** Take Snapshot. */
@Component({
  selector: 'app-snapshot-dialog',
  imports: [ClarityModule, FormsModule],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="md">
      <h3 class="modal-title">Take Snapshot - {{ vm().name }}</h3>
      <div class="modal-body">
        @if (error()) {
          <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
            <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
          </clr-alert>
        }
        <form clrForm clrLayout="horizontal" (ngSubmit)="save()">
          <clr-input-container>
            <label>Name</label>
            <input clrInput name="name" [(ngModel)]="name" size="40" />
          </clr-input-container>
          <clr-textarea-container>
            <label>Description</label>
            <textarea clrTextarea name="description" rows="3" [(ngModel)]="description"></textarea>
          </clr-textarea-container>
          <clr-checkbox-container>
            <clr-checkbox-wrapper>
              <input type="checkbox" clrCheckbox name="memory" [(ngModel)]="memory" [disabled]="!poweredOn()" />
              <label>Include virtual machine's memory</label>
            </clr-checkbox-wrapper>
            <clr-checkbox-wrapper>
              <input type="checkbox" clrCheckbox name="quiesce" [(ngModel)]="quiesce" [disabled]="!poweredOn() || memory" />
              <label>Quiesce guest file system (requires VM tools)</label>
            </clr-checkbox-wrapper>
          </clr-checkbox-container>
        </form>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" (click)="close()">Cancel</button>
        <button type="button" class="btn btn-primary" [clrLoading]="busy()" [disabled]="!name.trim()" (click)="save()">Create</button>
      </div>
    </clr-modal>
  `,
})
export class SnapshotDialogComponent {
  private readonly api = inject(ApiService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);

  readonly vm = input.required<Vm>();

  protected name = `VM Snapshot ${new Date().toLocaleString('en-US')}`;
  protected description = '';
  protected memory = false;
  protected quiesce = false;
  protected readonly error = signal('');
  protected readonly busy = signal(false);

  protected poweredOn(): boolean {
    return this.vm().power_state === 'poweredOn';
  }

  protected close(): void {
    this.dialogs.takeSnapshot.set(null);
  }

  protected async save(): Promise<void> {
    if (!this.name.trim() || this.busy()) return;
    const vm = this.vm();
    const on = this.poweredOn();
    this.error.set('');
    this.busy.set(true);
    try {
      await this.tasks.run('Create virtual machine snapshot', vm.name, () =>
        this.api.createSnapshot(vm.host_id, vm.id, {
          name: this.name.trim(),
          description: this.description.trim(),
          memory: on && this.memory,
          quiesce: on && !this.memory && this.quiesce,
        }),
      );
      this.close();
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.busy.set(false);
      void this.inventory.refresh();
    }
  }
}
