import { Component, OnInit, computed, inject, input } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ActionsService, BytesPipe, DialogService, InventoryService, TaskService, Vm } from '../../core';

/** Clone a powered-off VM (or deploy from a template) on the same host. */
@Component({
  selector: 'app-clone-vm-dialog',
  imports: [ClarityModule, FormsModule, BytesPipe],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="md">
      <h3 class="modal-title">
        {{ template() ? 'Clone to Template' : vm().template ? 'New VM from Template' : 'Clone Virtual Machine' }}
        <span class="fc-muted">| {{ vm().name }}</span>
      </h3>
      <div class="modal-body">
        <form clrForm clrLayout="horizontal" (ngSubmit)="clone()">
          <clr-input-container>
            <label>{{ template() ? 'Template name' : 'Virtual machine name' }}</label>
            <input clrInput name="name" [(ngModel)]="name" size="40" maxlength="80" />
            @if (nameTaken()) {
              <clr-control-helper>A virtual machine with this name already exists on the host</clr-control-helper>
            }
          </clr-input-container>
          <clr-select-container>
            <label>Datastore</label>
            <select clrSelect name="datastore" [(ngModel)]="datastore">
              @for (d of datastores(); track d.name) {
                <option [value]="d.name">{{ d.name }} ({{ d.free_bytes | bytes }} free)</option>
              }
            </select>
          </clr-select-container>
          @if (!template()) {
            <clr-checkbox-container>
              <clr-checkbox-wrapper>
                <input type="checkbox" clrCheckbox name="powerOn" [(ngModel)]="powerOn" />
                <label>Power on the virtual machine after creation</label>
              </clr-checkbox-wrapper>
            </clr-checkbox-container>
          }
        </form>
        <p class="fc-muted">
          The copy is created on {{ hostName() }} with a new identity (UUID and MAC addresses). Disks keep their provisioning; snapshots are consolidated into
          the copy. To place it on another host, clone it here and then migrate it.
        </p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" (click)="close()">Cancel</button>
        <button type="button" class="btn btn-primary" [disabled]="!valid()" (click)="clone()">Clone</button>
      </div>
    </clr-modal>
  `,
})
export class CloneVmDialogComponent implements OnInit {
  private readonly actions = inject(ActionsService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);

  readonly vm = input.required<Vm>();
  /** True when the copy must become a template. */
  readonly template = input(false);

  protected readonly datastores = computed(() => (this.inventory.datastoresByHost()[this.vm().host_id] ?? []).filter((d) => d.accessible));
  protected readonly hostName = computed(() => this.inventory.hostName(this.vm().host_id));

  protected name = '';
  protected datastore = '';
  protected powerOn = false;

  ngOnInit(): void {
    const vm = this.vm();
    this.name = this.template() ? `${vm.name}-template` : `${vm.name}-clone`;
    const current = vm.datastores?.[0] ?? '';
    this.datastore = this.datastores().some((d) => d.name === current) ? current : (this.datastores()[0]?.name ?? '');
  }

  protected nameTaken(): boolean {
    return this.inventory.vmsOf(this.vm().host_id).some((v) => v.name === this.name.trim());
  }

  protected valid(): boolean {
    const n = this.name.trim();
    return !!n && !/["'/\\]/.test(n) && !this.nameTaken() && !!this.datastore;
  }

  protected close(): void {
    this.dialogs.cloneVm.set(null);
  }

  protected clone(): void {
    if (!this.valid()) return;
    this.tasks.panelOpen.set(true);
    // Followed in Recent Tasks; the dialog does not need to stay open.
    void this.actions.cloneVm(this.vm(), { name: this.name.trim(), datastore: this.datastore, power_on: this.powerOn && !this.template(), template: this.template() });
    this.close();
  }
}
