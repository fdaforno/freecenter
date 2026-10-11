import { Component, OnDestroy, OnInit, inject, signal } from '@angular/core';
import { ClarityModule } from '@clr/angular';
import { RouterLink } from '@angular/router';

import { Alarm, ApiService, errorMessage } from '../core';

/** Alarms card for the root Summary: active conditions across all hosts. */
@Component({
  selector: 'app-alarms',
  imports: [ClarityModule, RouterLink],
  template: `
    <div class="alarms-toolbar">
      <button class="btn btn-sm btn-outline" (click)="refresh()" [disabled]="refreshing()">
        <cds-icon shape="refresh" [class.spin]="refreshing()"></cds-icon>
        Refresh
      </button>
    </div>

    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    } @else if (loaded()) {
      @if (alarms().length === 0) {
        <p class="fc-muted">No active alarms.</p>
      } @else {
        <ul class="alarms">
          @for (a of alarms(); track $index) {
            <li [class.danger]="a.severity === 'danger'" [class.warning]="a.severity === 'warning'">
              <cds-icon
                [shape]="a.severity === 'danger' ? 'error-standard' : 'warning-standard'"
                [status]="a.severity === 'danger' ? 'danger' : 'warning'"
                solid size="14">
              </cds-icon>
              <a [routerLink]="link(a)" class="target">{{ a.target }}</a>
              <span class="msg">{{ a.message }}</span>
              <a [routerLink]="['/host', a.host_id]" class="host">{{ a.host_name }}</a>
            </li>
          }
        </ul>
      }
    } @else {
      <clr-spinner clrMedium></clr-spinner>
    }
  `,
  styles: [`
    :host { display: block; }
    .alarms-toolbar { margin-bottom: 0.4rem; }
    .alarms-toolbar .btn { margin: 0; }
    .alarms { list-style: none; margin: 0; padding: 0; font-size: 0.65rem; }
    .alarms li {
      display: grid;
      grid-template-columns: auto auto 1fr auto;
      gap: 0.35rem;
      align-items: baseline;
      padding: 0.3rem 0.4rem;
      border-radius: 3px;
      margin-bottom: 0.25rem;
    }
    .alarms li.danger { background: #fbeae7; }
    .alarms li.warning { background: #fdf6e3; }
    .alarms .target { font-weight: 600; color: #0072a3; text-decoration: none; }
    .alarms .target:hover { text-decoration: underline; }
    .alarms .msg { color: #333; overflow-wrap: anywhere; }
    .alarms .host { color: #0072a3; text-decoration: none; }
    .alarms .host:hover { text-decoration: underline; }
    .spin { animation: fc-spin 1s linear infinite; }
    @keyframes fc-spin { to { transform: rotate(360deg); } }
  `],
})
export class AlarmsComponent implements OnInit, OnDestroy {
  private readonly api = inject(ApiService);

  protected readonly alarms = signal<Alarm[]>([]);
  protected readonly loaded = signal(false);
  protected readonly error = signal('');
  protected readonly refreshing = signal(false);
  private timer: ReturnType<typeof setInterval> | null = null;

  ngOnInit(): void {
    void this.load();
    this.timer = setInterval(() => {
      if (!document.hidden) void this.load();
    }, REFRESH_MS);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  /** Manual refresh: triggers an immediate probe on the server. */
  protected async refresh(): Promise<void> {
    if (this.refreshing()) return;
    this.refreshing.set(true);
    try {
      this.alarms.set(await this.api.refreshAlarms());
      this.loaded.set(true);
      this.error.set('');
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.refreshing.set(false);
    }
  }

  private async load(): Promise<void> {
    try {
      this.alarms.set(await this.api.alarms());
      this.loaded.set(true);
      this.error.set('');
    } catch (e) {
      this.error.set(errorMessage(e));
    }
  }

  protected link(a: Alarm): unknown[] {
    return ['/datastore', a.host_id, a.target];
  }
}

const REFRESH_MS = 60000;
