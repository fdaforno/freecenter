import { Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { ActionsService, DialogService, InventoryService, MbPipe, PowerPipe, Vm } from '../core';
import { ObjectIconComponent } from './object-icon.component';

interface VmRow {
  vm: Vm;
  name: string;
  state: string;
  guest: string;
  ip: string;
  cpus: number;
  memory: number;
  hostCpu: number;
  hostMem: number;
  host: string;
}

/** Virtual machine list, as in every vSphere "VMs" tab. */
@Component({
  selector: 'app-vm-grid',
  imports: [ClarityModule, RouterLink, ObjectIconComponent, MbPipe, PowerPipe],
  template: `
    <clr-datagrid class="datagrid-compact" [clrDgLoading]="loading()">
      <clr-dg-column [clrDgField]="'name'" [clrDgSortOrder]="1">Name</clr-dg-column>
      <clr-dg-column [clrDgField]="'state'">State</clr-dg-column>
      <clr-dg-column [clrDgField]="'guest'">Guest OS</clr-dg-column>
      <clr-dg-column [clrDgField]="'ip'">IP Address</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'cpus'">CPUs</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'memory'">Memory Size</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'hostCpu'">Host CPU</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'hostMem'">Host Mem</clr-dg-column>
      @if (showHost()) {
        <clr-dg-column [clrDgField]="'host'">Host</clr-dg-column>
      }

      <clr-dg-placeholder>No virtual machines found</clr-dg-placeholder>

      <clr-dg-row *clrDgItems="let row of rows(); trackBy: trackRow" [clrDgItem]="row">
        <clr-dg-action-overflow>
          <button class="action-item" [disabled]="row.vm.power_state === 'poweredOn' || !!row.vm.template" (click)="actions.vmPower(row.vm, 'on')">Power On</button>
          <button class="action-item" [disabled]="row.vm.power_state !== 'poweredOn'" (click)="actions.vmPower(row.vm, 'shutdown_guest')">Shut Down Guest OS</button>
          <button class="action-item" [disabled]="row.vm.power_state !== 'poweredOn'" (click)="actions.vmPower(row.vm, 'off')">Power Off</button>
          <button class="action-item" [disabled]="row.vm.power_state !== 'poweredOn'" (click)="actions.openConsole(row.vm)">Launch Web Console</button>
          <button class="action-item" [disabled]="row.vm.power_state !== 'poweredOn'" (click)="actions.openConsoleTab(row.vm.host_id, row.vm.id)">Launch Web Console in New Tab</button>
          <button class="action-item" (click)="dialogs.takeSnapshot.set(row.vm)">Take Snapshot...</button>
          <button class="action-item" [disabled]="row.vm.power_state !== 'poweredOff'" (click)="dialogs.cloneVm.set({ vm: row.vm, template: false })">Clone...</button>
          <button class="action-item" (click)="dialogs.migrateVm.set(row.vm)">Migrate...</button>
          <button class="action-item" (click)="dialogs.editVm.set(row.vm)">Edit Settings...</button>
          <button class="action-item" (click)="actions.deleteVm(row.vm)">Delete from Disk</button>
        </clr-dg-action-overflow>
        <clr-dg-cell>
          <app-object-icon shape="vm" [vm]="row.vm" />
          <a [routerLink]="['/vm', row.vm.host_id, row.vm.id]">{{ row.name }}</a>
        </clr-dg-cell>
        <clr-dg-cell>{{ row.vm.template ? 'Template' : (row.vm.power_state | power) }}</clr-dg-cell>
        <clr-dg-cell>{{ row.guest }}</clr-dg-cell>
        <clr-dg-cell>{{ row.ip }}</clr-dg-cell>
        <clr-dg-cell>{{ row.cpus }}</clr-dg-cell>
        <clr-dg-cell>{{ row.memory | mb }}</clr-dg-cell>
        <clr-dg-cell>{{ row.hostCpu }} MHz</clr-dg-cell>
        <clr-dg-cell>{{ row.hostMem | mb }}</clr-dg-cell>
        @if (showHost()) {
          <clr-dg-cell><a [routerLink]="['/host', row.vm.host_id]">{{ row.host }}</a></clr-dg-cell>
        }
      </clr-dg-row>

      <clr-dg-footer>{{ rows().length }} items</clr-dg-footer>
    </clr-datagrid>
  `,
})
export class VmGridComponent {
  protected readonly actions = inject(ActionsService);
  protected readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);

  readonly vms = input.required<Vm[]>();
  readonly showHost = input(false);
  readonly loading = input(false);
  /** Free-text filter coming from the global search box. */
  readonly filter = input('');

  protected readonly rows = computed<VmRow[]>(() => {
    const q = this.filter().trim().toLowerCase();
    return this.vms()
      .map((vm) => ({
        vm,
        name: vm.name,
        state: vm.power_state,
        guest: vm.guest_full_name || vm.guest_id || '',
        ip: vm.ip_address ?? '',
        cpus: vm.num_cpus,
        memory: vm.memory_mb,
        hostCpu: vm.cpu_usage_mhz ?? 0,
        hostMem: vm.host_memory_mb ?? 0,
        host: this.inventory.hostName(vm.host_id),
      }))
      .filter((r) => !q || [r.name, r.guest, r.ip, r.host].some((s) => s.toLowerCase().includes(q)));
  });

  protected trackRow(_: number, row: VmRow): string {
    return `${row.vm.host_id}/${row.vm.id}`;
  }
}
