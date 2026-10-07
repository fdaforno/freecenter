import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { RouterLink } from '@angular/router';

import { ApiService, HostNetwork, InventoryService } from '../../core';
import { NotFoundComponent } from '../../shared/not-found.component';
import { ObjectHeaderComponent, ObjectTab } from '../../shared/object-header.component';

const TABS: ObjectTab[] = [{ id: 'summary', label: 'Summary' }];

@Component({
  selector: 'app-network-page',
  imports: [RouterLink, ObjectHeaderComponent, NotFoundComponent],
  template: `
    @if (entry(); as e) {
      <div class="fc-page">
        <app-object-header icon="network-globe" [title]="e.item.name" [base]="['/network', e.host.id, e.item.name]" [tabs]="tabs" />

        <div class="fc-page-body">
          <div class="fc-cards">
            <div class="card">
              <div class="card-header">Network Details</div>
              <div class="card-block">
                <dl class="fc-props">
                  <dt>Type</dt>
                  <dd>{{ e.item.type || '--' }}</dd>
                  <dt>Host</dt>
                  <dd><a [routerLink]="['/host', e.host.id]">{{ e.host.name }}</a></dd>
                  @if (portGroup(); as pg) {
                    <dt>Virtual Switch</dt>
                    <dd>{{ pg.vswitch }}</dd>
                    <dt>VLAN ID</dt>
                    <dd>{{ pg.vlan === 0 ? 'None (0)' : pg.vlan === 4095 ? 'All (4095)' : pg.vlan }}</dd>
                    <dt>Uplinks</dt>
                    <dd>{{ uplinks() || 'None (internal only)' }}</dd>
                    <dt>Virtual Machines</dt>
                    <dd>{{ pg.vms }}</dd>
                  }
                </dl>
              </div>
              <div class="card-footer">
                <a class="btn btn-sm btn-link" [routerLink]="['/host', e.host.id, 'networks']">Manage Virtual Switches and Port Groups</a>
              </div>
            </div>
          </div>
        </div>
      </div>
    } @else {
      <app-not-found kind="Network" />
    }
  `,
  styles: `
    .card-footer .btn {
      margin: 0;
    }
  `,
})
export class NetworkPage {
  private readonly api = inject(ApiService);
  private readonly inventory = inject(InventoryService);
  protected readonly tabs = TABS;

  readonly hid = input.required<string>();
  readonly name = input.required<string>();
  /** Bound by the router; this object only has a Summary tab. */
  readonly tab = input<string>();

  protected readonly entry = computed(() => this.inventory.allNetworks().find((n) => n.host.id === this.hid() && n.item.name === this.name()));

  /** Switches and port groups of the host: where this network's VLAN and uplinks come from. */
  private readonly hostNetwork = signal<HostNetwork | null>(null);
  protected readonly portGroup = computed(() => this.hostNetwork()?.port_groups.find((p) => p.name === this.name()));
  protected readonly uplinks = computed(() => {
    const sw = this.hostNetwork()?.vswitches.find((s) => s.name === this.portGroup()?.vswitch);
    return sw?.uplinks.join(', ') ?? '';
  });

  constructor() {
    effect(() => {
      const hid = this.hid();
      untracked(() => {
        this.hostNetwork.set(null);
        // The details are a bonus: without them the page still shows the network.
        this.api
          .hostNetwork(hid)
          .then((n) => {
            if (this.hid() === hid) this.hostNetwork.set(n);
          })
          .catch(() => undefined);
      });
    });
  }
}
