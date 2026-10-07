import { Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { BytesPipe, Datastore, HostScoped } from '../core';
import { ObjectIconComponent } from './object-icon.component';

interface DatastoreRow {
  hostId: string;
  host: string;
  name: string;
  status: string;
  type: string;
  capacity: number;
  free: number;
}

@Component({
  selector: 'app-datastore-grid',
  imports: [ClarityModule, RouterLink, ObjectIconComponent, BytesPipe],
  template: `
    <clr-datagrid class="datagrid-compact">
      <clr-dg-column [clrDgField]="'name'" [clrDgSortOrder]="1">Name</clr-dg-column>
      <clr-dg-column [clrDgField]="'status'">Status</clr-dg-column>
      <clr-dg-column [clrDgField]="'type'">Type</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'capacity'">Capacity</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'free'">Free</clr-dg-column>
      @if (showHost()) {
        <clr-dg-column [clrDgField]="'host'">Host</clr-dg-column>
      }

      <clr-dg-placeholder>No datastores found</clr-dg-placeholder>

      <clr-dg-row *clrDgItems="let row of rows(); trackBy: trackRow" [clrDgItem]="row">
        <clr-dg-cell>
          <app-object-icon shape="storage" />
          <a [routerLink]="['/datastore', row.hostId, row.name]">{{ row.name }}</a>
        </clr-dg-cell>
        <clr-dg-cell>
          <cds-icon [shape]="row.status === 'Normal' ? 'success-standard' : 'error-standard'" [status]="row.status === 'Normal' ? 'success' : 'danger'" solid size="14"></cds-icon>
          {{ row.status }}
        </clr-dg-cell>
        <clr-dg-cell>{{ row.type }}</clr-dg-cell>
        <clr-dg-cell>{{ row.capacity | bytes }}</clr-dg-cell>
        <clr-dg-cell>{{ row.free | bytes }}</clr-dg-cell>
        @if (showHost()) {
          <clr-dg-cell><a [routerLink]="['/host', row.hostId]">{{ row.host }}</a></clr-dg-cell>
        }
      </clr-dg-row>

      <clr-dg-footer>{{ rows().length }} items</clr-dg-footer>
    </clr-datagrid>
  `,
})
export class DatastoreGridComponent {
  readonly datastores = input.required<HostScoped<Datastore>[]>();
  readonly showHost = input(false);

  protected readonly rows = computed<DatastoreRow[]>(() =>
    this.datastores().map(({ host, item }) => ({
      hostId: host.id,
      host: host.name,
      name: item.name,
      status: item.accessible ? 'Normal' : 'Inaccessible',
      type: item.type,
      capacity: item.capacity_bytes,
      free: item.free_bytes,
    })),
  );

  protected trackRow(_: number, row: DatastoreRow): string {
    return `${row.hostId}/${row.name}`;
  }
}
