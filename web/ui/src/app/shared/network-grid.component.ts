import { Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { HostScoped, Network } from '../core';
import { ObjectIconComponent } from './object-icon.component';

interface NetworkRow {
  hostId: string;
  host: string;
  name: string;
  type: string;
}

@Component({
  selector: 'app-network-grid',
  imports: [ClarityModule, RouterLink, ObjectIconComponent],
  template: `
    <clr-datagrid class="datagrid-compact">
      <clr-dg-column [clrDgField]="'name'" [clrDgSortOrder]="1">Name</clr-dg-column>
      <clr-dg-column [clrDgField]="'type'">Type</clr-dg-column>
      @if (showHost()) {
        <clr-dg-column [clrDgField]="'host'">Host</clr-dg-column>
      }

      <clr-dg-placeholder>No networks found</clr-dg-placeholder>

      <clr-dg-row *clrDgItems="let row of rows(); trackBy: trackRow" [clrDgItem]="row">
        <clr-dg-cell>
          <app-object-icon shape="network-globe" />
          <a [routerLink]="['/network', row.hostId, row.name]">{{ row.name }}</a>
        </clr-dg-cell>
        <clr-dg-cell>{{ row.type }}</clr-dg-cell>
        @if (showHost()) {
          <clr-dg-cell><a [routerLink]="['/host', row.hostId]">{{ row.host }}</a></clr-dg-cell>
        }
      </clr-dg-row>

      <clr-dg-footer>{{ rows().length }} items</clr-dg-footer>
    </clr-datagrid>
  `,
})
export class NetworkGridComponent {
  readonly networks = input.required<HostScoped<Network>[]>();
  readonly showHost = input(false);

  protected readonly rows = computed<NetworkRow[]>(() =>
    this.networks().map(({ host, item }) => ({
      hostId: host.id,
      host: host.name,
      name: item.name,
      type: item.type,
    })),
  );

  protected trackRow(_: number, row: NetworkRow): string {
    return `${row.hostId}/${row.name}`;
  }
}
