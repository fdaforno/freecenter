import { DatePipe } from '@angular/common';
import { Component, computed, inject, input } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ActionsService, DialogService, InventoryService, UptimePipe, formatBytes, formatMb } from '../../core';
import { DatastoreGridComponent } from '../../shared/datastore-grid.component';
import { HostActionsComponent } from '../../shared/host-actions.component';
import { MonitorPanelComponent } from '../../shared/monitor-panel.component';
import { NotFoundComponent } from '../../shared/not-found.component';
import { ObjectHeaderComponent, ObjectTab } from '../../shared/object-header.component';
import { UsageMeterComponent } from '../../shared/usage-meter.component';
import { VmGridComponent } from '../../shared/vm-grid.component';
import { HostConfigureComponent } from './host-configure.component';
import { HostNetworkComponent } from './host-network.component';

const TABS: ObjectTab[] = [
  { id: 'summary', label: 'Summary' },
  { id: 'monitor', label: 'Monitor' },
  { id: 'configure', label: 'Configure' },
  { id: 'vms', label: 'VMs' },
  { id: 'datastores', label: 'Datastores' },
  { id: 'networks', label: 'Networks' },
];

@Component({
  selector: 'app-host-page',
  imports: [
    ClarityModule,
    DatePipe,
    UptimePipe,
    ObjectHeaderComponent,
    HostActionsComponent,
    UsageMeterComponent,
    VmGridComponent,
    DatastoreGridComponent,
    NotFoundComponent,
    MonitorPanelComponent,
    HostConfigureComponent,
    HostNetworkComponent,
  ],
  template: `
    @if (host(); as h) {
      <div class="fc-page">
        <app-object-header icon="host" [title]="h.name" [host]="h" [base]="['/host', h.id]" [tabs]="tabs">
          <app-host-actions [host]="h" />
        </app-object-header>

        <div class="fc-page-body">
          @if (!h.status?.connected) {
            <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
              <clr-alert-item>
                <span class="alert-text">Host is not responding: {{ h.status?.last_error || 'connection failed' }}</span>
                <div class="alert-actions">
                  <button class="btn alert-action" (click)="dialogs.hostForm.set({ host: h })">Edit Connection Settings</button>
                </div>
              </clr-alert-item>
            </clr-alert>
          }

          @switch (tab()) {
            @case ('monitor') {
              <app-monitor-panel [hostId]="h.id" />
            }
            @case ('vms') {
              <div class="fc-toolbar">
                <button class="btn btn-sm btn-outline" [disabled]="!h.status?.connected" (click)="dialogs.newVm.set({ hostId: h.id })">
                  <cds-icon shape="plus"></cds-icon> New Virtual Machine
                </button>
              </div>
              <app-vm-grid [vms]="vms()" />
            }
            @case ('datastores') {
              <app-datastore-grid [datastores]="datastores()" />
            }
            @case ('networks') {
              <app-host-network [hostId]="h.id" />
            }
            @case ('configure') {
              <app-host-configure [host]="h" />
            }
            @default {
              @if (h.status; as st) {
                <div class="fc-cards">
                  <div class="card">
                    <div class="card-header">Host Details</div>
                    <div class="card-block">
                      <dl class="fc-props">
                        <dt>Hypervisor</dt>
                        <dd>{{ st.product }} {{ st.version }}{{ st.build ? ', ' + st.build : '' }}</dd>
                        <dt>Processor Type</dt>
                        <dd>{{ st.cpu_model || '--' }}</dd>
                        <dt>Processor Sockets</dt>
                        <dd>{{ st.cpus || '--' }}</dd>
                        <dt>Cores</dt>
                        <dd>{{ st.cpu_cores || '--' }}</dd>
                        <dt>Virtual Machines</dt>
                        <dd>{{ st.vms_total ?? 0 }} ({{ st.vms_running ?? 0 }} powered on)</dd>
                        <dt>State</dt>
                        <dd>{{ !st.connected ? 'Not responding' : st.maintenance_mode ? 'Maintenance Mode' : 'Connected' }}</dd>
                        <dt>Uptime</dt>
                        <dd>{{ st.uptime_seconds | uptime }}</dd>
                      </dl>
                    </div>
                  </div>
                  <div class="card">
                    <div class="card-header">Capacity and Usage</div>
                    <div class="card-block">
                      <dl class="fc-props cpu">
                        <dt>CPU</dt>
                        <dd>{{ st.cpu_cores || 0 }} cores{{ st.cpu_mhz ? ' x ' + st.cpu_mhz + ' MHz' : '' }}</dd>
                      </dl>
                      <app-usage-meter label="Memory" [used]="st.memory_used_mb ?? 0" [total]="st.memory_total_mb ?? 0" [format]="formatMb" />
                      <app-usage-meter label="Storage" [used]="storage().used" [total]="storage().total" [format]="formatBytes" />
                    </div>
                  </div>
                  <div class="card">
                    <div class="card-header">Configuration</div>
                    <div class="card-block">
                      <dl class="fc-props">
                        <dt>License</dt>
                        <dd>{{ st.license_name || '--' }}</dd>
                        <dt>Maintenance Mode</dt>
                        <dd>{{ st.maintenance_mode ? 'Yes' : 'No' }}</dd>
                        <dt>SSH</dt>
                        <dd>{{ st.ssh_ok ? 'Running' : 'Not available' }}</dd>
                        <dt>Boot Time</dt>
                        <dd>{{ (st.boot_time | date: 'medium') || '--' }}</dd>
                        <dt>Address</dt>
                        <dd>{{ h.address }}</dd>
                      </dl>
                    </div>
                  </div>
                </div>
              }
            }
          }
        </div>
      </div>
    } @else {
      <app-not-found kind="Host" />
    }
  `,
  styles: `
    .cpu {
      margin-bottom: 0.7rem;
    }
    .card-footer .btn {
      margin: 0;
    }
  `,
})
export class HostPage {
  private readonly inventory = inject(InventoryService);
  protected readonly actions = inject(ActionsService);
  protected readonly dialogs = inject(DialogService);
  protected readonly tabs = TABS;
  protected readonly formatMb = formatMb;
  protected readonly formatBytes = formatBytes;

  readonly hid = input.required<string>();
  readonly tab = input.required<string>();

  protected readonly host = computed(() => this.inventory.host(this.hid()));
  protected readonly vms = computed(() => this.inventory.vmsByHost()[this.hid()] ?? []);
  protected readonly datastores = computed(() => this.inventory.allDatastores().filter((d) => d.host.id === this.hid()));
  protected readonly storage = computed(() => {
    const total = this.datastores().reduce((s, d) => s + d.item.capacity_bytes, 0);
    const free = this.datastores().reduce((s, d) => s + d.item.free_bytes, 0);
    return { total, used: total - free };
  });
}
