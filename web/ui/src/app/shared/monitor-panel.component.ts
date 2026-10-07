import { DatePipe } from '@angular/common';
import { Component, OnDestroy, effect, inject, input, signal, untracked } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ApiService, HostEvent, PerfData, errorMessage } from '../core';
import { LineChartComponent } from './line-chart.component';

type MonitorView = 'performance' | 'events';

/** ESXi adds a real-time sample every 20 seconds. */
const REFRESH_MS = 20000;

/** The Monitor tab of a host or virtual machine: real-time performance and the event log. */
@Component({
  selector: 'app-monitor-panel',
  imports: [ClarityModule, DatePipe, LineChartComponent],
  template: `
    <div class="btn-group btn-sm fc-toolbar">
      <div class="radio btn">
        <input type="radio" name="monitorView" id="mon-perf" [checked]="view() === 'performance'" (change)="view.set('performance')" />
        <label for="mon-perf">Performance</label>
      </div>
      <div class="radio btn">
        <input type="radio" name="monitorView" id="mon-events" [checked]="view() === 'events'" (change)="view.set('events')" />
        <label for="mon-events">Events</label>
      </div>
    </div>

    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    }

    @if (view() === 'performance') {
      @if (perf(); as p) {
        @if (p.series.length === 0) {
          <div class="fc-empty">No performance data: real-time statistics exist only while the {{ vmId() ? 'virtual machine is powered on' : 'host is running' }}.</div>
        } @else {
          <p class="fc-muted">Real-time, last hour (one sample every {{ p.interval_seconds }} seconds)</p>
          <div class="charts">
            @for (s of p.series; track s.key) {
              <div class="card">
                <div class="card-block">
                  <app-line-chart [title]="s.label" [unit]="s.unit" [times]="p.times" [values]="s.values" [max]="s.unit === '%' ? 100 : undefined" />
                </div>
              </div>
            }
          </div>
        }
      } @else if (!error()) {
        <div class="fc-empty"><clr-spinner clrMedium></clr-spinner></div>
      }
    } @else {
      <table class="table table-compact events">
        <thead>
          <tr>
            <th class="left">Description</th>
            <th class="left">Type</th>
            <th class="left">Date Time</th>
            <th class="left">Target</th>
            <th class="left">User</th>
          </tr>
        </thead>
        <tbody>
          @for (e of events(); track $index) {
            <tr>
              <td class="left">{{ e.message }}</td>
              <td class="left nowrap">
                @switch (e.category) {
                  @case ('error') {
                    <cds-icon shape="error-standard" status="danger" solid size="14"></cds-icon> Error
                  }
                  @case ('warning') {
                    <cds-icon shape="warning-standard" status="warning" solid size="14"></cds-icon> Warning
                  }
                  @default {
                    <cds-icon shape="info-standard" status="info" solid size="14"></cds-icon> Information
                  }
                }
              </td>
              <td class="left nowrap">{{ e.time | date: 'MM/dd/yyyy, h:mm:ss a' }}</td>
              <td class="left">{{ e.target }}</td>
              <td class="left">{{ e.user }}</td>
            </tr>
          } @empty {
            <tr>
              <td colspan="5" class="fc-muted">{{ eventsLoaded() ? 'No events' : 'Loading...' }}</td>
            </tr>
          }
        </tbody>
      </table>
    }
  `,
  styles: `
    :host {
      display: flex;
      flex-direction: column;
      flex: 1 1 auto;
      min-height: 0;
    }
    /* Always two columns: the four charts form a 2 x 2 grid that fills the tab. */
    .charts {
      flex: 1 1 auto;
      min-height: 0;
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      grid-auto-rows: minmax(0, 1fr);
      gap: 0.6rem;
    }
    .charts .card-block {
      height: 100%;
    }
    @media (max-width: 760px) {
      .charts {
        grid-template-columns: minmax(0, 1fr);
        grid-auto-rows: auto;
      }
    }
    .card {
      margin: 0;
    }
    .events {
      margin-top: 0;
    }
    .nowrap {
      white-space: nowrap;
    }
  `,
})
export class MonitorPanelComponent implements OnDestroy {
  private readonly api = inject(ApiService);
  private timer: ReturnType<typeof setInterval> | null = null;

  readonly hostId = input.required<string>();
  /** Set to monitor one virtual machine instead of the host. */
  readonly vmId = input<string>();

  protected readonly view = signal<MonitorView>('performance');
  protected readonly perf = signal<PerfData | null>(null);
  protected readonly events = signal<HostEvent[]>([]);
  protected readonly eventsLoaded = signal(false);
  protected readonly error = signal('');

  constructor() {
    // Reload at once when the object or the view changes, then keep refreshing.
    effect(() => {
      this.hostId();
      this.vmId();
      this.view();
      untracked(() => {
        this.perf.set(null);
        this.events.set([]);
        this.eventsLoaded.set(false);
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
    const hid = this.hostId();
    const vmid = this.vmId();
    const view = this.view();
    try {
      if (view === 'performance') {
        const data = await this.api.performance(hid, vmid);
        if (this.isCurrent(hid, vmid, view)) this.perf.set(data);
      } else {
        const data = await this.api.events(hid, vmid);
        if (this.isCurrent(hid, vmid, view)) {
          this.events.set(data);
          this.eventsLoaded.set(true);
        }
      }
      this.error.set('');
    } catch (e) {
      if (this.isCurrent(hid, vmid, view)) this.error.set(errorMessage(e));
    }
  }

  private isCurrent(hid: string, vmid: string | undefined, view: MonitorView): boolean {
    return this.hostId() === hid && this.vmId() === vmid && this.view() === view;
  }
}
