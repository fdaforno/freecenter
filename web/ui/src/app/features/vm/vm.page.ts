import { DatePipe } from '@angular/common';
import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { ActionsService, ApiService, DialogService, InventoryService, MbPipe, PowerPipe, UptimePipe, Vm, formatMb } from '../../core';
import { DatastoreGridComponent } from '../../shared/datastore-grid.component';
import { NetworkGridComponent } from '../../shared/network-grid.component';
import { MonitorPanelComponent } from '../../shared/monitor-panel.component';
import { NotFoundComponent } from '../../shared/not-found.component';
import { ObjectHeaderComponent, ObjectTab } from '../../shared/object-header.component';
import { UsageMeterComponent } from '../../shared/usage-meter.component';
import { VmActionsComponent } from '../../shared/vm-actions.component';
import { VmSnapshotsComponent } from './vm-snapshots.component';

const TABS: ObjectTab[] = [
  { id: 'summary', label: 'Summary' },
  { id: 'monitor', label: 'Monitor' },
  { id: 'datastores', label: 'Datastores' },
  { id: 'networks', label: 'Networks' },
  { id: 'snapshots', label: 'Snapshots' },
];

@Component({
  selector: 'app-vm-page',
  imports: [
    ClarityModule,
    RouterLink,
    DatePipe,
    MbPipe,
    PowerPipe,
    UptimePipe,
    ObjectHeaderComponent,
    VmActionsComponent,
    UsageMeterComponent,
    DatastoreGridComponent,
    NetworkGridComponent,
    NotFoundComponent,
    MonitorPanelComponent,
    VmSnapshotsComponent,
  ],
  template: `
    @if (vm(); as v) {
      <div class="fc-page">
        <app-object-header icon="vm" [title]="v.name" [vm]="v" [base]="['/vm', v.host_id, v.id]" [tabs]="tabs">
          <button class="btn btn-icon btn-link btn-sm quick" title="Power On" [disabled]="v.power_state === 'poweredOn' || !!v.template" (click)="actions.vmPower(v, 'on')">
            <cds-icon shape="play" solid status="success"></cds-icon>
          </button>
          <button class="btn btn-icon btn-link btn-sm quick" title="Power Off" [disabled]="v.power_state !== 'poweredOn'" (click)="actions.vmPower(v, 'off')">
            <cds-icon shape="stop" solid status="danger"></cds-icon>
          </button>
          <button class="btn btn-icon btn-link btn-sm quick" title="Open console" [disabled]="v.power_state !== 'poweredOn'" (click)="actions.openConsole(v)">
            <cds-icon shape="display"></cds-icon>
          </button>
          <button class="btn btn-icon btn-link btn-sm quick" title="Edit Settings" (click)="dialogs.editVm.set(v)">
            <cds-icon shape="cog"></cds-icon>
          </button>
          <button class="btn btn-icon btn-link btn-sm quick" title="Take Snapshot" (click)="dialogs.takeSnapshot.set(v)">
            <cds-icon shape="camera"></cds-icon>
          </button>
          <span class="divider"></span>
          <app-vm-actions [vm]="v" />
        </app-object-header>

        <div class="fc-page-body">
          @switch (tab()) {
            @case ('monitor') {
              <app-monitor-panel [hostId]="v.host_id" [vmId]="v.id" />
            }
            @case ('datastores') {
              <app-datastore-grid [datastores]="datastores()" />
            }
            @case ('networks') {
              <app-network-grid [networks]="networks()" />
            }
            @case ('snapshots') {
              <app-vm-snapshots [vm]="v" />
            }
            @default {
              <div class="fc-cards">
                <div class="card">
                  <div class="card-header">Guest OS</div>
                  <div class="card-block guest">
                    <div class="console-box">
                      <div class="console" [class.on]="v.power_state === 'poweredOn'">
                        <cds-icon shape="vm" size="36"></cds-icon>
                        <span>{{ v.power_state | power }}</span>
                      </div>
                      <button class="btn btn-sm btn-link" [disabled]="v.power_state !== 'poweredOn'" (click)="actions.openConsole(v)">Open console</button>
                      <button class="btn btn-sm btn-link" [disabled]="v.power_state !== 'poweredOn'" (click)="actions.openConsoleTab(v.host_id, v.id)">
                        Open in New Tab <cds-icon shape="pop-out" size="12"></cds-icon>
                      </button>
                    </div>
                    <dl class="fc-props">
                      <dt>Power Status</dt>
                      <dd>{{ v.template ? 'Template' : (v.power_state | power) }}</dd>
                      <dt>Guest OS</dt>
                      <dd>{{ v.guest_full_name || v.guest_id || '--' }}</dd>
                      <dt>Guest tools</dt>
                      <dd>{{ v.tools_status || '--' }}</dd>
                      <dt>DNS Name</dt>
                      <dd>{{ v.guest_hostname || '--' }}</dd>
                      <dt>IP Addresses</dt>
                      <dd>{{ v.ip_address || '--' }}</dd>
                      <dt>Uptime</dt>
                      <dd>{{ v.uptime_seconds | uptime }}</dd>
                    </dl>
                  </div>
                </div>

                <div class="card">
                  <div class="card-header">Resource usage</div>
                  <div class="card-block">
                    <dl class="fc-props cpu">
                      <dt>CPU</dt>
                      <dd>{{ v.cpu_usage_mhz ?? 0 }} MHz used, {{ v.num_cpus }} CPU(s) allocated</dd>
                    </dl>
                    <app-usage-meter label="Memory" [used]="v.guest_memory_mb ?? 0" [total]="v.memory_mb" [format]="formatMb" />
                    <dl class="fc-props">
                      <dt>Host Memory</dt>
                      <dd>{{ v.host_memory_mb ?? 0 | mb }} consumed</dd>
                      <dt>Storage</dt>
                      <dd>{{ provisionedGb() }} GB provisioned</dd>
                    </dl>
                  </div>
                </div>

                <div class="card">
                  <div class="card-header">Hardware</div>
                  <div class="card-block">
                    <dl class="fc-props">
                      <dt>CPU</dt>
                      <dd>{{ v.num_cpus }} CPU(s){{ v.cores_per_socket ? ', ' + v.cores_per_socket + ' core(s) per socket' : '' }}</dd>
                      <dt>Memory</dt>
                      <dd>{{ v.memory_mb | mb }}</dd>
                      @for (d of v.disks ?? []; track d.key) {
                        <dt>{{ d.label || 'Hard disk' }}</dt>
                        <dd>{{ d.capacity_gb }} GB | {{ d.thin ? 'Thin Provision' : 'Thick Provision' }} | {{ d.datastore }}</dd>
                      }
                      @for (n of v.nics ?? []; track n.key) {
                        <dt>{{ n.label || 'Network adapter' }}</dt>
                        <dd>{{ n.network }} ({{ n.connected ? 'connected' : 'disconnected' }}) | {{ n.adapter_type }} | {{ n.mac_address }}</dd>
                      }
                      @for (c of v.cdroms ?? []; track c.key) {
                        <dt>{{ c.label || 'CD/DVD drive' }}</dt>
                        <dd>{{ c.iso || 'Client Device (no media)' }}</dd>
                      }
                      @if (v.usb_controller) {
                        <dt>USB controller</dt>
                        <dd>{{ v.usb_controller === 'usb3' ? 'USB 3.1' : 'USB 2.0' }}</dd>
                      }
                      <dt>Firmware</dt>
                      <dd>{{ v.firmware === 'efi' ? 'EFI' : 'BIOS' }}</dd>
                      <dt>Hardware version</dt>
                      <dd>{{ v.hardware_version || '--' }}</dd>
                    </dl>
                  </div>
                  <div class="card-footer">
                    <button class="btn btn-sm btn-link" (click)="dialogs.editVm.set(v)">Edit</button>
                  </div>
                </div>

                <div class="card">
                  <div class="card-header">Placement</div>
                  <div class="card-block">
                    <dl class="fc-props">
                      <dt>Host</dt>
                      <dd><a [routerLink]="['/host', v.host_id]">{{ hostName() }}</a></dd>
                      <dt>Networks</dt>
                      <dd>
                        @for (n of v.networks ?? []; track n) {
                          <div><a [routerLink]="['/network', v.host_id, n]">{{ n }}</a></div>
                        } @empty {
                          --
                        }
                      </dd>
                      <dt>Storage</dt>
                      <dd>
                        @for (d of v.datastores ?? []; track d) {
                          <div><a [routerLink]="['/datastore', v.host_id, d]">{{ d }}</a></div>
                        } @empty {
                          --
                        }
                      </dd>
                    </dl>
                  </div>
                </div>

                <div class="card">
                  <div class="card-header">Files and snapshots</div>
                  <div class="card-block">
                    <dl class="fc-props">
                      <dt>VM Config File</dt>
                      <dd>{{ v.vmx_path || '--' }}</dd>
                      <dt>UUID</dt>
                      <dd>{{ v.uuid || '--' }}</dd>
                      <dt>Snapshots</dt>
                      <dd><a [routerLink]="['/vm', v.host_id, v.id, 'snapshots']">{{ v.snapshot_count }}</a></dd>
                      <dt>Last Boot</dt>
                      <dd>{{ (v.boot_time | date: 'medium') || '--' }}</dd>
                    </dl>
                  </div>
                </div>

                <div class="card">
                  <div class="card-header">Notes</div>
                  <div class="card-block notes">{{ v.annotation || 'No notes' }}</div>
                  <div class="card-footer">
                    <button class="btn btn-sm btn-link" (click)="dialogs.editVm.set(v)">Edit</button>
                  </div>
                </div>
              </div>
            }
          }
        </div>
      </div>
    } @else {
      <app-not-found kind="Virtual machine" />
    }
  `,
  styles: `
    .quick {
      margin: 0;
      min-width: 1.2rem;
      padding: 0 0.2rem;
    }
    .divider {
      width: 1px;
      height: 1.2rem;
      background: #cdd4d9;
      margin: 0 0.3rem;
    }
    .guest {
      display: flex;
      gap: 0.8rem;
      align-items: flex-start;
    }
    .guest .fc-props {
      flex: 1;
      grid-template-columns: 5.5rem 1fr;
    }
    .console-box {
      flex: 0 0 auto;
    }
    .console-box .btn {
      display: block;
      margin: 0.2rem 0 0;
      padding: 0;
    }
    .console {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 0.3rem;
      width: 6.5rem;
      height: 5rem;
      background: #1b2a32;
      color: #9fb2bd;
      font-size: 0.55rem;
      text-transform: uppercase;
    }
    .console.on {
      color: #d7e0e5;
    }
    .cpu {
      margin-bottom: 0.7rem;
    }
    .notes {
      white-space: pre-wrap;
      font-size: 0.65rem;
    }
    .card-footer .btn {
      margin: 0;
    }
  `,
})
export class VmPage {
  private readonly api = inject(ApiService);
  private readonly inventory = inject(InventoryService);
  protected readonly actions = inject(ActionsService);
  protected readonly dialogs = inject(DialogService);
  protected readonly tabs = TABS;
  protected readonly formatMb = formatMb;

  readonly hid = input.required<string>();
  readonly vmid = input.required<string>();
  readonly tab = input.required<string>();

  constructor() {
    // Reload when another VM is routed and after every inventory refresh (which follows each task).
    effect(() => {
      const hid = this.hid();
      const vmid = this.vmid();
      this.inventory.revision();
      untracked(() => void this.loadDetail(hid, vmid));
    });
  }

  private async loadDetail(hid: string, vmid: string): Promise<void> {
    // On failure the summary from the inventory stays visible (or "not found" once it is gone).
    const vm = await this.api.vm(hid, vmid).catch(() => null);
    if (this.hid() === hid && this.vmid() === vmid) this.detail.set(vm && { ...vm, host_id: hid });
  }

  /** Full VM (hardware, networks, UUID): the inventory list only carries a summary. */
  private readonly detail = signal<Vm | null>(null);
  protected readonly vm = computed(() => {
    const d = this.detail();
    if (d && d.host_id === this.hid() && d.id === this.vmid()) return d;
    return this.inventory.vmsOf(this.hid()).find((v) => v.id === this.vmid());
  });
  protected readonly hostName = computed(() => this.inventory.hostName(this.hid()));
  protected readonly provisionedGb = computed(() => (this.vm()?.disks ?? []).reduce((s, d) => s + d.capacity_gb, 0));
  protected readonly datastores = computed(() => {
    const names = this.vm()?.datastores ?? [];
    return this.inventory.allDatastores().filter((d) => d.host.id === this.hid() && names.includes(d.item.name));
  });
  protected readonly networks = computed(() => {
    const names = this.vm()?.networks ?? [];
    return this.inventory.allNetworks().filter((n) => n.host.id === this.hid() && names.includes(n.item.name));
  });
}
