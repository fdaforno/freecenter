import { Component, OnDestroy, effect, inject, input, signal, untracked } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ApiService, DatastoreHealth, DiskHealth, errorMessage } from '../../core';

/** Card for the datastore Summary: SMART/NVMe health of each backing device. */
@Component({
  selector: 'app-datastore-health',
  imports: [ClarityModule],
  template: `
    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    } @else if (sshDisabled()) {
      <p class="fc-muted">SSH is not enabled on this host: disk health (SMART/NVMe) is only reachable over SSH.</p>
    } @else if (health(); as h) {
      @if (h.devices.length === 0) {
        <p class="fc-muted">No backing devices found for this datastore.</p>
      } @else {
        <table class="table">
          <thead>
            <tr>
              <th class="left col-device">Device</th>
              <th class="left col-health">Health</th>
              <th class="right col-num">Realloc</th>
              <th class="right col-num">Pending</th>
              <th class="right col-num">Uncorr</th>
              <th class="right col-num">Media</th>
              <th class="right col-used">Used</th>
              <th class="right col-temp">Temp</th>
            </tr>
          </thead>
          <tbody>
            @for (d of h.devices; track d.device_id) {
              <tr [class.fc-row-danger]="isDanger(d)" [class.fc-row-warn]="isWarn(d)">
                <td class="left">
                  <span class="device-name" [title]="d.display_name || d.device_id">{{ d.display_name || d.device_id }}</span>
                  @if (d.form_factor) { <span class="fc-muted">({{ d.form_factor }})</span> }
                  @if (d.unsupported) { <span class="fc-muted">— health not exposed</span> }
                </td>
                <td class="left">
                  @if (d.smart_status) {
                    <span [class.fc-text-danger]="smartBad(d)" [class.fc-text-ok]="!smartBad(d)">{{ d.smart_status }}</span>
                  } @else {
                    <span class="fc-muted">--</span>
                  }
                </td>
                <td class="right" [class.fc-text-danger]="d.reallocated_sectors! > 0">{{ d.reallocated_sectors }}</td>
                <td class="right" [class.fc-text-danger]="d.pending_sectors! > 0">{{ d.pending_sectors }}</td>
                <td class="right" [class.fc-text-danger]="d.uncorrectable! > 0">{{ d.uncorrectable }}</td>
                <td class="right" [class.fc-text-danger]="d.media_errors! > 0">{{ d.media_errors }}</td>
                <td class="right" [class.fc-text-warn]="(d.percentage_used ?? 0) >= 80" [class.fc-text-danger]="(d.percentage_used ?? 0) >= 95">
                  {{ d.percentage_used ?? 0 }}%
                </td>
                <td class="right" [class.fc-text-danger]="tempDanger(d)" [class.fc-text-warn]="tempWarnOnly(d)">
                  @if (d.temp_ok && d.temperature! > 0) { {{ d.temperature }}°C } @else { <span class="fc-muted">--</span> }
                </td>
              </tr>
            }
          </tbody>
        </table>
        <p class="fc-muted legend">
          <span class="fc-swatch danger"></span> compromising
          <span class="fc-swatch warn"></span> warning
        </p>
      }
    } @else if (!error()) {
      <clr-spinner clrMedium></clr-spinner>
    }
  `,
  styles: [`
    :host { display: block; min-width: 0; overflow: hidden; }
    .table {
      margin: 0;
      font-size: 0.6rem;
      table-layout: fixed;
      width: 100%;
    }
    /* Device column takes the remaining space; the rest are narrow. */
    .col-device { width: auto; }
    .col-num    { width: 3.2rem; }
    .col-health { width: 3.5rem; }
    .col-used   { width: 2.8rem; }
    .col-temp   { width: 2.8rem; }
    .device-name {
      display: block;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .fc-text-danger { color: #c21d00; font-weight: 600; }
    .fc-text-warn { color: #c27b00; font-weight: 600; }
    .fc-text-ok { color: #3c8500; }
    .fc-row-danger { background: #fbeae7; }
    .fc-row-warn { background: #fdf6e3; }
    .legend { margin: 0.4rem 0 0; display: flex; gap: 0.6rem; align-items: center; }
    .fc-swatch { display: inline-block; width: 10px; height: 10px; border-radius: 2px; margin-right: 0.2rem; }
    .fc-swatch.danger { background: #c21d00; }
    .fc-swatch.warn { background: #efc006; }
  `],
})
export class DatastoreHealthComponent implements OnDestroy {
  private readonly api = inject(ApiService);

  readonly hostId = input.required<string>();
  readonly datastore = input.required<string>();

  protected readonly health = signal<DatastoreHealth | null>(null);
  protected readonly error = signal('');
  protected readonly sshDisabled = signal(false);
  private timer: ReturnType<typeof setInterval> | null = null;

  constructor() {
    // Reload when the target changes.
    effect(() => {
      this.hostId();
      this.datastore();
      untracked(() => {
        this.health.set(null);
        this.error.set('');
        this.sshDisabled.set(false);
        void this.load();
      });
    });
    this.timer = setInterval(() => {
      if (!document.hidden) void this.load();
    }, REFRESH_MS);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  private async load(): Promise<void> {
    try {
      const h = await this.api.datastoreHealth(this.hostId(), this.datastore());
      this.health.set(h);
      this.error.set('');
    } catch (e) {
      const msg = errorMessage(e);
      // SSH disabled is reported as a conflict, like other SSH-dependent calls.
      if (/ssh is not enabled/i.test(msg)) {
        this.sshDisabled.set(true);
        this.error.set('');
      } else {
        this.error.set(msg);
      }
    }
  }

  protected smartBad(d: DiskHealth): boolean {
    return !!d.smart_status && d.smart_status.toLowerCase() !== 'ok';
  }
  protected tempDanger(d: DiskHealth): boolean {
    return !!d.temperature && d.temperature >= TEMP_DANGER;
  }
  protected tempWarnOnly(d: DiskHealth): boolean {
    return !!d.temperature && d.temperature >= TEMP_WARN && d.temperature < TEMP_DANGER;
  }
  protected isDanger(d: DiskHealth): boolean {
    return this.smartBad(d) || (d.reallocated_sectors ?? 0) > 0 || (d.pending_sectors ?? 0) > 0 || (d.uncorrectable ?? 0) > 0 || (d.media_errors ?? 0) > 0 || this.tempDanger(d);
  }
  protected isWarn(d: DiskHealth): boolean {
    return !this.isDanger(d) && (this.tempWarnOnly(d) || (d.percentage_used ?? 0) >= 80);
  }
}

const REFRESH_MS = 60000;
const TEMP_WARN = 50;
const TEMP_DANGER = 65;
