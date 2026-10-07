import { DatePipe } from '@angular/common';
import { Component, effect, inject, input, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ActionsService, ApiService, HostTime, errorMessage } from '../../core';

/** A clock this far from the browser's is worth pointing out. */
const SKEW_WARNING_MS = 60_000;

/** Time configuration: the host clock and its NTP settings. */
@Component({
  selector: 'app-host-time',
  imports: [ClarityModule, FormsModule, DatePipe],
  template: `
    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    }
    @if (time(); as t) {
      @if (skewMinutes() !== 0) {
        <clr-alert clrAlertType="warning" [clrAlertClosable]="false">
          <clr-alert-item>
            <span class="alert-text">
              The host clock is {{ skewMinutes() > 0 ? 'ahead of' : 'behind' }} this computer by about {{ absSkew() }} minute(s).
              {{ t.ntp_enabled ? '' : 'NTP is not enabled on the host.' }}
            </span>
          </clr-alert-item>
        </clr-alert>
      }
      <dl class="fc-props">
        <dt>Date &amp; Time on host</dt>
        <dd>{{ t.now | date: 'MM/dd/yyyy, h:mm:ss a' }} (shown in your time zone)</dd>
        <dt>Network Time Protocol</dt>
        <dd>{{ t.ntp_enabled ? 'Enabled' : 'Disabled' }}</dd>
        <dt>NTP Servers</dt>
        <dd>{{ t.ntp_servers.join(', ') || '--' }}</dd>
      </dl>

      <form clrForm clrLayout="horizontal" (ngSubmit)="save()">
        <clr-checkbox-container>
          <label>NTP</label>
          <clr-checkbox-wrapper>
            <input type="checkbox" clrCheckbox name="enabled" [(ngModel)]="enabled" />
            <label>Enable (synchronize the host clock with the servers below)</label>
          </clr-checkbox-wrapper>
        </clr-checkbox-container>
        <clr-input-container>
          <label>NTP Servers</label>
          <input clrInput name="servers" [(ngModel)]="servers" size="50" placeholder="0.pool.ntp.org, 1.pool.ntp.org" />
          <clr-control-helper>Comma separated host names or IP addresses</clr-control-helper>
        </clr-input-container>
        <button type="submit" class="btn btn-sm btn-primary" [disabled]="enabled && serverList().length === 0">Save</button>
      </form>
      <p class="fc-muted">After enabling NTP, start the "NTP Daemon" service (and set it to start with the host) under Services.</p>
    } @else if (!error()) {
      <div class="fc-empty"><clr-spinner clrMedium></clr-spinner></div>
    }
  `,
  styles: `
    .fc-props {
      margin-bottom: 0.6rem;
      max-width: 40rem;
    }
    .btn {
      margin-top: 0.6rem;
    }
  `,
})
export class HostTimeComponent {
  private readonly api = inject(ApiService);
  private readonly actions = inject(ActionsService);

  readonly hostId = input.required<string>();

  protected readonly time = signal<HostTime | null>(null);
  protected readonly skewMinutes = signal(0);
  protected readonly error = signal('');
  protected enabled = false;
  protected servers = '';

  constructor() {
    effect(() => {
      this.hostId();
      untracked(() => {
        this.time.set(null);
        void this.load();
      });
    });
  }

  protected absSkew(): number {
    return Math.abs(this.skewMinutes());
  }

  protected serverList(): string[] {
    return this.servers
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
  }

  private async load(): Promise<void> {
    const hid = this.hostId();
    try {
      const t = await this.api.hostTime(hid);
      if (this.hostId() !== hid) return;
      const skew = new Date(t.now).getTime() - Date.now();
      this.skewMinutes.set(Math.abs(skew) > SKEW_WARNING_MS ? Math.round(skew / 60_000) : 0);
      this.enabled = t.ntp_enabled;
      this.servers = t.ntp_servers.join(', ');
      this.time.set(t);
      this.error.set('');
    } catch (e) {
      this.error.set(errorMessage(e));
    }
  }

  protected async save(): Promise<void> {
    await this.actions.track('Update NTP configuration', 'Time configuration', () => this.api.setHostTime(this.hostId(), this.enabled, this.serverList()));
    await this.load();
  }
}
