import { Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { DialogService, InventoryService, formatBytes } from '../../core';
import { NotFoundComponent } from '../../shared/not-found.component';
import { ObjectHeaderComponent, ObjectTab } from '../../shared/object-header.component';
import { UsageMeterComponent } from '../../shared/usage-meter.component';
import { VmGridComponent } from '../../shared/vm-grid.component';
import { DatastoreFilesComponent } from './datastore-files.component';

const TABS: ObjectTab[] = [
  { id: 'summary', label: 'Summary' },
  { id: 'files', label: 'Files' },
  { id: 'vms', label: 'VMs' },
];

@Component({
  selector: 'app-datastore-page',
  imports: [ClarityModule, RouterLink, ObjectHeaderComponent, UsageMeterComponent, VmGridComponent, NotFoundComponent, DatastoreFilesComponent],
  template: `
    @if (entry(); as e) {
      <div class="fc-page">
        <app-object-header icon="storage" [title]="e.item.name" [base]="['/datastore', e.host.id, e.item.name]" [tabs]="tabs">
          <clr-dropdown>
            <button class="btn btn-link btn-sm fc-actions" clrDropdownTrigger>
              Actions <cds-icon shape="angle" direction="down" size="12"></cds-icon>
            </button>
            <clr-dropdown-menu *clrIfOpen clrPosition="bottom-left">
              <h4 class="dropdown-header">Actions - {{ e.item.name }}</h4>
              <button clrDropdownItem [routerLink]="['/datastore', e.host.id, e.item.name, 'files']">Browse Files</button>
              <button clrDropdownItem (click)="dialogs.isoDownload.set({ hostId: e.host.id, datastore: e.item.name })">Download ISO to Datastore...</button>
            </clr-dropdown-menu>
          </clr-dropdown>
        </app-object-header>

        <div class="fc-page-body">
          @switch (tab()) {
            @case ('files') {
              <app-datastore-files [hostId]="e.host.id" [datastore]="e.item.name" />
            }
            @case ('vms') {
              <app-vm-grid [vms]="vms()" />
            }
            @default {
              <div class="fc-cards">
                <div class="card">
                  <div class="card-header">Details</div>
                  <div class="card-block">
                    <dl class="fc-props">
                      <dt>Type</dt>
                      <dd>{{ e.item.type || '--' }}</dd>
                      <dt>Status</dt>
                      <dd>{{ e.item.accessible ? 'Normal' : 'Inaccessible' }}</dd>
                      <dt>Host</dt>
                      <dd><a [routerLink]="['/host', e.host.id]">{{ e.host.name }}</a></dd>
                      <dt>Virtual Machines</dt>
                      <dd>{{ vms().length }}</dd>
                      <dt>URL</dt>
                      <dd>{{ e.item.url || '--' }}</dd>
                    </dl>
                  </div>
                </div>
                <div class="card">
                  <div class="card-header">Capacity and Usage</div>
                  <div class="card-block">
                    <app-usage-meter label="Storage" [used]="e.item.capacity_bytes - e.item.free_bytes" [total]="e.item.capacity_bytes" [format]="formatBytes" />
                  </div>
                </div>
              </div>
            }
          }
        </div>
      </div>
    } @else {
      <app-not-found kind="Datastore" />
    }
  `,
})
export class DatastorePage {
  private readonly inventory = inject(InventoryService);
  protected readonly dialogs = inject(DialogService);
  protected readonly tabs = TABS;
  protected readonly formatBytes = formatBytes;

  readonly hid = input.required<string>();
  readonly name = input.required<string>();
  readonly tab = input.required<string>();

  protected readonly entry = computed(() => this.inventory.allDatastores().find((d) => d.host.id === this.hid() && d.item.name === this.name()));
  protected readonly vms = computed(() => (this.inventory.vmsByHost()[this.hid()] ?? []).filter((vm) => (vm.datastores ?? []).includes(this.name())));
}
