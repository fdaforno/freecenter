import { Component, effect, inject, input, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { ActionsService, ApiService, DialogService, HostNetwork, InventoryService, PortGroup, VSwitch, errorMessage } from '../../core';

/** Virtual switches of a host with their port groups and uplinks. */
@Component({
  selector: 'app-host-network',
  imports: [ClarityModule, FormsModule, RouterLink],
  template: `
    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    }
    <div class="fc-toolbar">
      <button class="btn btn-sm btn-outline" (click)="openSwitchForm()"><cds-icon shape="plus"></cds-icon> Add Virtual Switch...</button>
      <button class="btn btn-sm btn-outline" [disabled]="!net()?.vswitches?.length" (click)="openGroupForm(net()?.vswitches?.[0]?.name ?? '')">
        <cds-icon shape="plus"></cds-icon> Add Port Group...
      </button>
      <button class="btn btn-sm btn-link" title="Refresh" (click)="load()"><cds-icon shape="refresh"></cds-icon></button>
    </div>

    @if (net(); as n) {
      @for (sw of n.vswitches; track sw.name) {
        <div class="card">
          <div class="card-header">
            <cds-icon shape="network-switch" size="16"></cds-icon> Standard Switch: {{ sw.name }}
            <span class="fc-muted meta">MTU {{ sw.mtu }} · Uplinks: {{ uplinkText(sw, n) }}</span>
            <span class="actions">
              <button class="btn btn-sm btn-link" (click)="openGroupForm(sw.name)">Add Port Group...</button>
              <button class="btn btn-sm btn-link" [disabled]="sw.port_groups.length > 0" [title]="sw.port_groups.length > 0 ? 'Remove its port groups first' : ''" (click)="removeSwitch(sw)">Remove</button>
            </span>
          </div>
          <div class="card-block">
            <table class="table table-compact">
              <thead>
                <tr>
                  <th class="left">Port Group</th>
                  <th class="left">VLAN ID</th>
                  <th class="left">Virtual Machines</th>
                  <th class="left">Used by</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                @for (pg of groupsOf(sw.name, n); track pg.name) {
                  <tr>
                    <td class="left">
                      @if (pg.management) {
                        {{ pg.name }}
                      } @else {
                        <a [routerLink]="['/network', hostId(), pg.name]">{{ pg.name }}</a>
                      }
                    </td>
                    <td class="left">{{ vlanText(pg.vlan) }}</td>
                    <td class="left">{{ pg.vms }}</td>
                    <td class="left">{{ pg.management ? 'VMkernel (host management)' : 'Virtual machines' }}</td>
                    <td class="right">
                      <button class="btn btn-sm btn-link" [disabled]="pg.management" (click)="editVlan(pg)">Edit VLAN...</button>
                      <button class="btn btn-sm btn-link" [disabled]="pg.management || pg.vms > 0" (click)="removeGroup(pg)">Remove</button>
                    </td>
                  </tr>
                } @empty {
                  <tr>
                    <td colspan="5" class="fc-muted">No port groups</td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        </div>
      }

      <div class="card">
        <div class="card-header">Physical adapters</div>
        <div class="card-block">
          <table class="table table-compact">
            <thead>
              <tr>
                <th class="left">Device</th>
                <th class="left">Actual Speed</th>
                <th class="left">MAC Address</th>
                <th class="left">Driver</th>
                <th class="left">Switch</th>
              </tr>
            </thead>
            <tbody>
              @for (nic of n.nics; track nic.device) {
                <tr>
                  <td class="left">{{ nic.device }}</td>
                  <td class="left">{{ nic.speed_mb ? nic.speed_mb + ' Mb' : 'Down' }}</td>
                  <td class="left">{{ nic.mac }}</td>
                  <td class="left">{{ nic.driver }}</td>
                  <td class="left">{{ switchOf(nic.device, n) || '--' }}</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      </div>
    } @else if (!error()) {
      <div class="fc-empty"><clr-spinner clrMedium></clr-spinner></div>
    }

    @if (groupForm(); as f) {
      <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="groupForm.set(null)" clrModalSize="md">
        <h3 class="modal-title">Add Port Group</h3>
        <div class="modal-body">
          <form clrForm clrLayout="horizontal" (ngSubmit)="addGroup()">
            <clr-select-container>
              <label>Virtual switch</label>
              <select clrSelect name="pgSwitch" [(ngModel)]="f.vswitch">
                @for (sw of net()?.vswitches ?? []; track sw.name) {
                  <option [value]="sw.name">{{ sw.name }}</option>
                }
              </select>
            </clr-select-container>
            <clr-input-container>
              <label>Network label</label>
              <input clrInput name="pgName" [(ngModel)]="f.name" maxlength="64" />
            </clr-input-container>
            <clr-input-container>
              <label>VLAN ID</label>
              <input clrInput type="number" name="pgVlan" min="0" max="4095" [(ngModel)]="f.vlan" />
              <clr-control-helper>0 = none, 4095 = all (trunk)</clr-control-helper>
            </clr-input-container>
          </form>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" (click)="groupForm.set(null)">Cancel</button>
          <button type="button" class="btn btn-primary" [disabled]="!validName(f.name) || !validVlan(f.vlan)" (click)="addGroup()">OK</button>
        </div>
      </clr-modal>
    }

    @if (switchForm(); as f) {
      <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="switchForm.set(null)" clrModalSize="md">
        <h3 class="modal-title">Add Virtual Switch</h3>
        <div class="modal-body">
          <form clrForm clrLayout="horizontal" (ngSubmit)="addSwitch()">
            <clr-input-container>
              <label>Name</label>
              <input clrInput name="swName" [(ngModel)]="f.name" maxlength="32" placeholder="vSwitch1" />
            </clr-input-container>
            <clr-select-container>
              <label>Uplink</label>
              <select clrSelect name="swUplink" [(ngModel)]="f.uplink">
                <option value="">None (internal only)</option>
                @for (nic of freeNics(); track nic) {
                  <option [value]="nic">{{ nic }}</option>
                }
              </select>
              <clr-control-helper>Only adapters not used by another switch are offered</clr-control-helper>
            </clr-select-container>
          </form>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" (click)="switchForm.set(null)">Cancel</button>
          <button type="button" class="btn btn-primary" [disabled]="!validName(f.name)" (click)="addSwitch()">OK</button>
        </div>
      </clr-modal>
    }
  `,
  styles: `
    .card {
      margin-top: 0;
      margin-bottom: 0.6rem;
    }
    .card-header {
      display: flex;
      align-items: center;
      gap: 0.4rem;
    }
    .meta {
      font-weight: 400;
      font-size: 0.6rem;
    }
    .actions {
      margin-left: auto;
    }
    .actions .btn,
    td .btn {
      margin: 0;
    }
    .table {
      margin: 0;
    }
    .right {
      text-align: right;
      white-space: nowrap;
    }
  `,
})
export class HostNetworkComponent {
  private readonly api = inject(ApiService);
  private readonly actions = inject(ActionsService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);

  readonly hostId = input.required<string>();

  protected readonly net = signal<HostNetwork | null>(null);
  protected readonly error = signal('');
  protected readonly groupForm = signal<{ vswitch: string; name: string; vlan: number } | null>(null);
  protected readonly switchForm = signal<{ name: string; uplink: string } | null>(null);

  constructor() {
    effect(() => {
      this.hostId();
      untracked(() => {
        this.net.set(null);
        void this.load();
      });
    });
  }

  protected groupsOf(vswitch: string, n: HostNetwork): PortGroup[] {
    return n.port_groups.filter((p) => p.vswitch === vswitch);
  }

  protected switchOf(nic: string, n: HostNetwork): string {
    return n.vswitches.find((s) => s.uplinks.includes(nic))?.name ?? '';
  }

  protected uplinkText(sw: VSwitch, n: HostNetwork): string {
    if (sw.uplinks.length === 0) return 'none (internal only)';
    return sw.uplinks
      .map((u) => {
        const speed = n.nics.find((x) => x.device === u)?.speed_mb;
        return `${u} (${speed ? speed + ' Mb' : 'down'})`;
      })
      .join(', ');
  }

  protected vlanText(vlan: number): string {
    if (vlan === 0) return 'None (0)';
    return vlan === 4095 ? 'All (4095)' : String(vlan);
  }

  protected freeNics(): string[] {
    const n = this.net();
    return n ? n.nics.map((x) => x.device).filter((d) => !this.switchOf(d, n)) : [];
  }

  protected validName(name: string): boolean {
    return /^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$/.test(name.trim());
  }

  protected validVlan(vlan: number): boolean {
    return Number.isInteger(Number(vlan)) && vlan >= 0 && vlan <= 4095;
  }

  protected async load(): Promise<void> {
    const hid = this.hostId();
    try {
      const n = await this.api.hostNetwork(hid);
      if (this.hostId() !== hid) return;
      this.net.set(n);
      this.error.set('');
    } catch (e) {
      this.error.set(errorMessage(e));
    }
  }

  protected openGroupForm(vswitch: string): void {
    this.groupForm.set({ vswitch, name: '', vlan: 0 });
  }

  protected openSwitchForm(): void {
    this.switchForm.set({ name: '', uplink: '' });
  }

  protected async addGroup(): Promise<void> {
    const f = this.groupForm();
    if (!f || !this.validName(f.name) || !this.validVlan(f.vlan)) return;
    this.groupForm.set(null);
    await this.change('Add port group', f.name.trim(), () => this.api.addPortGroup(this.hostId(), f.name.trim(), f.vswitch, Number(f.vlan)));
  }

  protected async addSwitch(): Promise<void> {
    const f = this.switchForm();
    if (!f || !this.validName(f.name)) return;
    this.switchForm.set(null);
    await this.change('Add virtual switch', f.name.trim(), () => this.api.addVSwitch(this.hostId(), f.name.trim(), f.uplink ? [f.uplink] : []));
  }

  protected async editVlan(pg: PortGroup): Promise<void> {
    const value = await this.dialogs.prompt({
      title: `Edit VLAN - ${pg.name}`,
      message: 'VLAN ID of the port group: 0 = none, 4095 = all (trunk).',
      promptLabel: 'VLAN ID',
      promptDefault: String(pg.vlan),
      okLabel: 'OK',
    });
    if (value === null) return;
    const vlan = Number(value);
    if (!this.validVlan(vlan)) {
      this.error.set('VLAN ID must be a number between 0 and 4095');
      return;
    }
    await this.change('Update port group', pg.name, () => this.api.setPortGroupVlan(this.hostId(), pg.name, vlan));
  }

  protected async removeGroup(pg: PortGroup): Promise<void> {
    const ok = await this.dialogs.confirm({ title: 'Remove Port Group', message: `Remove port group "${pg.name}" from ${pg.vswitch}?`, okLabel: 'Remove', danger: true });
    if (ok) await this.change('Remove port group', pg.name, () => this.api.removePortGroup(this.hostId(), pg.name));
  }

  protected async removeSwitch(sw: VSwitch): Promise<void> {
    const ok = await this.dialogs.confirm({ title: 'Remove Virtual Switch', message: `Remove virtual switch "${sw.name}"?`, okLabel: 'Remove', danger: true });
    if (ok) await this.change('Remove virtual switch', sw.name, () => this.api.removeVSwitch(this.hostId(), sw.name));
  }

  /** Runs a network change as a task, then reloads this view (the inventory refreshes its network list too). */
  private async change(name: string, target: string, op: () => Promise<unknown>): Promise<void> {
    this.error.set('');
    await this.actions.track(name, target, op);
    await this.load();
    void this.inventory.refresh();
  }
}
