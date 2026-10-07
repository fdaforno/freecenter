import { Component, computed, inject, input } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { DialogService, InventoryService, formatBytes, formatMb } from '../../core';
import { DatastoreGridComponent } from '../../shared/datastore-grid.component';
import { HostGridComponent } from '../../shared/host-grid.component';
import { NetworkGridComponent } from '../../shared/network-grid.component';
import { ObjectHeaderComponent, ObjectTab } from '../../shared/object-header.component';
import { UsageMeterComponent } from '../../shared/usage-meter.component';
import { VmGridComponent } from '../../shared/vm-grid.component';
import { ProtectionComponent } from './protection.component';

const TABS: ObjectTab[] = [
  { id: 'summary', label: 'Summary' },
  { id: 'hosts', label: 'Hosts' },
  { id: 'vms', label: 'VMs' },
  { id: 'datastores', label: 'Datastores' },
  { id: 'networks', label: 'Networks' },
  { id: 'backups', label: 'Backups' },
  { id: 'schedules', label: 'Scheduled Tasks' },
];

/** The inventory root: what a vCenter Server object shows in the vSphere Client. */
@Component({
  selector: 'app-root-page',
  imports: [ClarityModule, ObjectHeaderComponent, UsageMeterComponent, HostGridComponent, VmGridComponent, DatastoreGridComponent, NetworkGridComponent, ProtectionComponent],
  template: `
    <div class="fc-page">
      <app-object-header icon="building" title="FreeCenter" [base]="['/root']" [tabs]="tabs">
        <clr-dropdown>
          <button class="btn btn-link btn-sm fc-actions" clrDropdownTrigger>
            Actions <cds-icon shape="angle" direction="down" size="12"></cds-icon>
          </button>
          <clr-dropdown-menu *clrIfOpen clrPosition="bottom-left">
            <h4 class="dropdown-header">Actions - FreeCenter</h4>
            <button clrDropdownItem (click)="dialogs.hostForm.set({})">Add Host...</button>
            <button clrDropdownItem [clrDisabled]="stats().hostsConnected === 0" (click)="dialogs.newVm.set({})">New Virtual Machine...</button>
          </clr-dropdown-menu>
        </clr-dropdown>
      </app-object-header>

      <div class="fc-page-body">
        @switch (tab()) {
          @case ('hosts') {
            <app-host-grid [hosts]="inventory.hosts()" />
          }
          @case ('vms') {
            <app-vm-grid [vms]="inventory.allVms()" [showHost]="true" [loading]="!inventory.loaded()" [filter]="q() ?? ''" />
          }
          @case ('datastores') {
            <app-datastore-grid [datastores]="inventory.allDatastores()" [showHost]="true" />
          }
          @case ('backups') {
            <app-protection view="backups" />
          }
          @case ('schedules') {
            <app-protection view="schedules" />
          }
          @case ('networks') {
            <app-network-grid [networks]="inventory.allNetworks()" [showHost]="true" />
          }
          @default {
            @if (inventory.error()) {
              <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
                <clr-alert-item><span class="alert-text">{{ inventory.error() }}</span></clr-alert-item>
              </clr-alert>
            }
            @if (inventory.loaded() && inventory.hosts().length === 0) {
              <clr-alert clrAlertType="info" [clrAlertClosable]="false">
                <clr-alert-item>
                  <span class="alert-text">No ESXi hosts are registered yet.</span>
                  <div class="alert-actions"><button class="btn alert-action" (click)="dialogs.hostForm.set({})">Add Host</button></div>
                </clr-alert-item>
              </clr-alert>
            }
            <div class="fc-cards">
              <div class="card">
                <div class="card-header">Inventory</div>
                <div class="card-block">
                  <dl class="fc-props">
                    <dt>Hosts</dt>
                    <dd>{{ stats().hosts }} ({{ stats().hostsConnected }} connected)</dd>
                    <dt>Virtual Machines</dt>
                    <dd>{{ stats().vms }}</dd>
                    <dt>Powered On</dt>
                    <dd>{{ stats().vmsOn }}</dd>
                    <dt>Powered Off</dt>
                    <dd>{{ stats().vmsOff }}</dd>
                    <dt>Suspended</dt>
                    <dd>{{ stats().vmsSuspended }}</dd>
                    <dt>Datastores</dt>
                    <dd>{{ inventory.allDatastores().length }}</dd>
                    <dt>Networks</dt>
                    <dd>{{ inventory.allNetworks().length }}</dd>
                  </dl>
                </div>
              </div>
              <div class="card">
                <div class="card-header">Capacity and Usage</div>
                <div class="card-block">
                  <dl class="fc-props cpu">
                    <dt>CPU</dt>
                    <dd>{{ stats().cores }} cores</dd>
                  </dl>
                  <app-usage-meter label="Memory" [used]="stats().memUsed" [total]="stats().memTotal" [format]="formatMb" />
                  <app-usage-meter label="Storage" [used]="stats().storageUsed" [total]="stats().storageTotal" [format]="formatBytes" />
                </div>
              </div>
            </div>
          }
        }
      </div>
    </div>
  `,
  styles: `
    .cpu {
      margin-bottom: 0.7rem;
    }
  `,
})
export class RootPage {
  protected readonly inventory = inject(InventoryService);
  protected readonly dialogs = inject(DialogService);
  protected readonly tabs = TABS;
  protected readonly formatMb = formatMb;
  protected readonly formatBytes = formatBytes;

  readonly tab = input.required<string>();
  /** Global search text (query parameter). */
  readonly q = input<string>();

  protected readonly stats = computed(() => {
    const hosts = this.inventory.hosts();
    const connected = hosts.filter((h) => h.status?.connected);
    const vms = this.inventory.allVms();
    // A datastore shared by several hosts is listed once per host: count it once.
    const stores = new Map(this.inventory.allDatastores().map((d) => [d.item.url || `${d.host.id}/${d.item.name}`, d.item]));
    let storageTotal = 0;
    let storageFree = 0;
    for (const d of stores.values()) {
      storageTotal += d.capacity_bytes;
      storageFree += d.free_bytes;
    }
    return {
      hosts: hosts.length,
      hostsConnected: connected.length,
      vms: vms.length,
      vmsOn: vms.filter((v) => v.power_state === 'poweredOn').length,
      vmsOff: vms.filter((v) => v.power_state === 'poweredOff').length,
      vmsSuspended: vms.filter((v) => v.power_state === 'suspended').length,
      cores: connected.reduce((s, h) => s + (h.status?.cpu_cores ?? 0), 0),
      memUsed: connected.reduce((s, h) => s + (h.status?.memory_used_mb ?? 0), 0),
      memTotal: connected.reduce((s, h) => s + (h.status?.memory_total_mb ?? 0), 0),
      storageUsed: storageTotal - storageFree,
      storageTotal,
    };
  });
}
