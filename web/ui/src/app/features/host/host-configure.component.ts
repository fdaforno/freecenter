import { DatePipe } from '@angular/common';
import { Component, inject, input, signal } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ActionsService, DialogService, Host } from '../../core';
import { EsxcliConsoleComponent } from './esxcli-console.component';
import { HostNetworkComponent } from './host-network.component';
import { HostServicesComponent } from './host-services.component';
import { HostTimeComponent } from './host-time.component';

type Section = 'connection' | 'services' | 'time' | 'esxcli' | 'vswitches';

const NAV: { group: string; items: { id: Section; label: string }[] }[] = [
  {
    group: 'System',
    items: [
      { id: 'connection', label: 'Connection' },
      { id: 'services', label: 'Services' },
      { id: 'time', label: 'Time Configuration' },
      { id: 'esxcli', label: 'ESXCLI Console' },
    ],
  },
  { group: 'Networking', items: [{ id: 'vswitches', label: 'Virtual switches' }] },
];

/** The Configure tab of a host: a section list on the left, the selected section on the right. */
@Component({
  selector: 'app-host-configure',
  imports: [ClarityModule, DatePipe, EsxcliConsoleComponent, HostServicesComponent, HostTimeComponent, HostNetworkComponent],
  template: `
    <nav aria-label="Configure sections">
      @for (g of nav; track g.group) {
        <div class="group">{{ g.group }}</div>
        @for (i of g.items; track i.id) {
          <button type="button" [class.active]="section() === i.id" (click)="section.set(i.id)">{{ i.label }}</button>
        }
      }
    </nav>
    <section>
      <h4>{{ title() }}</h4>
      @switch (section()) {
        @case ('services') {
          <app-host-services [hostId]="host().id" />
        }
        @case ('time') {
          <app-host-time [hostId]="host().id" />
        }
        @case ('esxcli') {
          <app-esxcli-console [hostId]="host().id" />
        }
        @case ('vswitches') {
          <app-host-network [hostId]="host().id" />
        }
        @default {
          <dl class="fc-props">
            <dt>Address</dt>
            <dd>{{ host().address }}</dd>
            <dt>User name</dt>
            <dd>{{ host().username }}</dd>
            <dt>SSH</dt>
            <dd>{{ host().ssh_enabled ? 'Enabled, port ' + (host().ssh_port || 22) : 'Disabled' }}</dd>
            <dt>SSH status</dt>
            <dd>{{ host().status?.ssh_ok ? 'Running' : 'Not available' }}</dd>
            <dt>Added</dt>
            <dd>{{ host().created_at | date: 'medium' }}</dd>
          </dl>
          <div class="fc-toolbar">
            <button class="btn btn-sm btn-outline" (click)="actions.testHost(host())">Test Connection</button>
            <button class="btn btn-sm btn-outline" (click)="dialogs.hostForm.set({ host: host() })">Edit...</button>
          </div>
        }
      }
    </section>
  `,
  styles: `
    :host {
      display: flex;
      gap: 0.9rem;
      align-items: flex-start;
      min-height: 100%;
    }
    nav {
      flex: 0 0 11rem;
      display: flex;
      flex-direction: column;
      background: #fff;
      border: 1px solid var(--fc-border);
      border-radius: 3px;
      padding: 0.3rem 0;
    }
    .group {
      padding: 0.3rem 0.6rem 0.1rem;
      font-size: 0.6rem;
      font-weight: 600;
      color: #21333b;
    }
    nav button {
      padding: 0.2rem 0.6rem 0.2rem 1.1rem;
      border: 0;
      background: none;
      text-align: left;
      font-size: 0.65rem;
      color: #454545;
      cursor: pointer;
    }
    nav button:hover {
      background: #f1f6f8;
    }
    nav button.active {
      background: #d8e3e9;
      color: #000;
    }
    section {
      flex: 1 1 auto;
      min-width: 0;
    }
    h4 {
      margin: 0 0 0.5rem;
      font-size: 0.8rem;
    }
    .fc-props {
      max-width: 36rem;
      margin-bottom: 0.6rem;
    }
  `,
})
export class HostConfigureComponent {
  protected readonly actions = inject(ActionsService);
  protected readonly dialogs = inject(DialogService);
  protected readonly nav = NAV;
  protected readonly section = signal<Section>('connection');

  readonly host = input.required<Host>();

  protected title(): string {
    return NAV.flatMap((g) => g.items).find((i) => i.id === this.section())?.label ?? '';
  }
}
