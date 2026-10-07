import { Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { Host, UptimePipe, percent } from '../core';
import { ObjectIconComponent } from './object-icon.component';

interface HostRow {
  host: Host;
  name: string;
  state: string;
  status: string;
  cores: number;
  memPct: number;
  vms: number;
  uptime: number;
  version: string;
}

@Component({
  selector: 'app-host-grid',
  imports: [ClarityModule, RouterLink, ObjectIconComponent, UptimePipe],
  template: `
    <clr-datagrid class="datagrid-compact">
      <clr-dg-column [clrDgField]="'name'" [clrDgSortOrder]="1">Name</clr-dg-column>
      <clr-dg-column [clrDgField]="'state'">State</clr-dg-column>
      <clr-dg-column [clrDgField]="'status'">Status</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'cores'">CPU Cores</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'memPct'">Consumed Memory %</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'vms'">VMs</clr-dg-column>
      <clr-dg-column [clrDgSortBy]="'uptime'">Uptime</clr-dg-column>
      <clr-dg-column [clrDgField]="'version'">Version</clr-dg-column>

      <clr-dg-placeholder>No hosts found. Use Actions &gt; Add Host to register an ESXi host.</clr-dg-placeholder>

      <clr-dg-row *clrDgItems="let row of rows(); trackBy: trackRow" [clrDgItem]="row">
        <clr-dg-cell>
          <app-object-icon shape="host" [host]="row.host" />
          <a [routerLink]="['/host', row.host.id]">{{ row.name }}</a>
        </clr-dg-cell>
        <clr-dg-cell>{{ row.state }}</clr-dg-cell>
        <clr-dg-cell [title]="row.host.status?.last_error ?? ''">
          <cds-icon [shape]="row.status === 'Normal' ? 'success-standard' : 'error-standard'" [status]="row.status === 'Normal' ? 'success' : 'danger'" solid size="14"></cds-icon>
          {{ row.status }}
        </clr-dg-cell>
        <clr-dg-cell>{{ row.cores }}</clr-dg-cell>
        <clr-dg-cell>
          <span class="bar"><span [style.width.%]="row.memPct"></span></span> {{ row.memPct }}%
        </clr-dg-cell>
        <clr-dg-cell>{{ row.vms }}</clr-dg-cell>
        <clr-dg-cell>{{ row.uptime | uptime }}</clr-dg-cell>
        <clr-dg-cell>{{ row.version }}</clr-dg-cell>
      </clr-dg-row>

      <clr-dg-footer>{{ rows().length }} items</clr-dg-footer>
    </clr-datagrid>
  `,
  styles: `
    .bar {
      display: inline-block;
      width: 80px;
      height: 8px;
      margin-right: 0.3rem;
      background: #e3e8eb;
      vertical-align: middle;
    }
    .bar span {
      display: block;
      height: 100%;
      background: #0072a3;
    }
  `,
})
export class HostGridComponent {
  readonly hosts = input.required<Host[]>();

  protected readonly rows = computed<HostRow[]>(() =>
    this.hosts().map((host) => {
      const st = host.status;
      const connected = !!st?.connected;
      return {
        host,
        name: host.name,
        state: !connected ? 'Not responding' : st?.maintenance_mode ? 'Maintenance Mode' : 'Connected',
        status: connected ? 'Normal' : 'Alert',
        cores: st?.cpu_cores ?? 0,
        memPct: percent(st?.memory_used_mb ?? 0, st?.memory_total_mb ?? 0),
        vms: st?.vms_total ?? 0,
        uptime: st?.uptime_seconds ?? 0,
        version: connected ? `${st?.product ?? ''} ${st?.version ?? ''} ${st?.build ? 'build-' + st.build : ''}`.trim() : '',
      };
    }),
  );

  protected trackRow(_: number, row: HostRow): string {
    return row.host.id;
  }
}
