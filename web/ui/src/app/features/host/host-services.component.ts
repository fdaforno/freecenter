import { Component, effect, inject, input, signal, untracked } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ActionsService, ApiService, HostService, errorMessage } from '../../core';

/** The service Hostwarden itself uses for every change on the host. */
const SSH_KEY = 'TSM-SSH';

/** Host services: state, start/stop/restart and startup policy. */
@Component({
  selector: 'app-host-services',
  imports: [ClarityModule],
  template: `
    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    }
    <div class="fc-toolbar">
      <button class="btn btn-sm btn-outline" [disabled]="!selected() || selected()!.running" (click)="act('start')">Start</button>
      <button class="btn btn-sm btn-outline" [disabled]="!selected() || !selected()!.running || isSsh()" (click)="act('stop')">Stop</button>
      <button class="btn btn-sm btn-outline" [disabled]="!selected() || !selected()!.running" (click)="act('restart')">Restart</button>
      <button class="btn btn-sm btn-outline" [disabled]="!selected() || (isSsh() && selected()!.policy === 'on')" (click)="togglePolicy()">
        {{ selected()?.policy === 'on' ? 'Do Not Start with Host' : 'Start and Stop with Host' }}
      </button>
      <button class="btn btn-sm btn-link" title="Refresh" (click)="load()"><cds-icon shape="refresh"></cds-icon></button>
    </div>
    <table class="table table-compact">
      <thead>
        <tr>
          <th class="left">Name</th>
          <th class="left">Daemon</th>
          <th class="left">Startup Policy</th>
        </tr>
      </thead>
      <tbody>
        @for (s of services(); track s.key) {
          <tr [class.active]="selectedKey() === s.key" (click)="selectedKey.set(s.key)">
            <td class="left">
              <button type="button" class="fc-link name" (click)="selectedKey.set(s.key)">{{ s.label }}</button>
            </td>
            <td class="left">{{ s.running ? 'Running' : 'Stopped' }}</td>
            <td class="left">{{ s.policy === 'on' ? 'Start and stop with host' : s.policy === 'automatic' ? 'Start and stop with port usage' : 'Start and stop manually' }}</td>
          </tr>
        } @empty {
          <tr>
            <td colspan="3" class="fc-muted">{{ loaded() ? 'No services' : 'Loading...' }}</td>
          </tr>
        }
      </tbody>
    </table>
    <p class="fc-muted">The SSH service cannot be stopped from here: Hostwarden applies every change to the host through it.</p>
  `,
  styles: `
    .table {
      margin-top: 0;
    }
    tr.active td {
      background: #d8e3e9;
    }
    .name {
      color: inherit;
    }
  `,
})
export class HostServicesComponent {
  private readonly api = inject(ApiService);
  private readonly actions = inject(ActionsService);

  readonly hostId = input.required<string>();

  protected readonly services = signal<HostService[]>([]);
  protected readonly selectedKey = signal<string | null>(null);
  protected readonly loaded = signal(false);
  protected readonly error = signal('');

  constructor() {
    effect(() => {
      this.hostId();
      untracked(() => {
        this.services.set([]);
        this.loaded.set(false);
        void this.load();
      });
    });
  }

  protected selected(): HostService | undefined {
    return this.services().find((s) => s.key === this.selectedKey());
  }

  protected isSsh(): boolean {
    return this.selectedKey() === SSH_KEY;
  }

  protected async load(): Promise<void> {
    const hid = this.hostId();
    try {
      const list = await this.api.services(hid);
      if (this.hostId() !== hid) return;
      this.services.set(list);
      this.error.set('');
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.loaded.set(true);
    }
  }

  protected async act(action: 'start' | 'stop' | 'restart'): Promise<void> {
    const s = this.selected();
    if (!s) return;
    const verb = { start: 'Start', stop: 'Stop', restart: 'Restart' }[action];
    await this.actions.track(`${verb} service`, s.label, () => this.api.setService(this.hostId(), s.key, { action }));
    await this.load();
  }

  protected async togglePolicy(): Promise<void> {
    const s = this.selected();
    if (!s) return;
    const policy = s.policy === 'on' ? 'off' : 'on';
    await this.actions.track('Update service startup policy', s.label, () => this.api.setService(this.hostId(), s.key, { policy }));
    await this.load();
  }
}
