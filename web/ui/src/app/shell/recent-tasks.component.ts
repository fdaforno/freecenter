import { DatePipe } from '@angular/common';
import { Component, computed, inject } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { TaskService } from '../core';

/** Bottom "Recent Tasks" panel. */
@Component({
  selector: 'app-recent-tasks',
  imports: [ClarityModule, DatePipe],
  template: `
    <button type="button" class="bar" [attr.aria-expanded]="tasks.panelOpen()" (click)="tasks.panelOpen.set(!tasks.panelOpen())">
      <cds-icon shape="angle" [direction]="tasks.panelOpen() ? 'down' : 'up'" size="14"></cds-icon>
      <span class="tab">Recent Tasks</span>
      @if (running() > 0) {
        <span class="badge badge-info">{{ running() }}</span>
      }
    </button>
    @if (tasks.panelOpen()) {
      <div class="body">
        <table class="table table-compact">
          <thead>
            <tr>
              <th class="left">Task Name</th>
              <th class="left">Target</th>
              <th class="left">Status</th>
              <th class="left">Details</th>
              <th class="left">Initiator</th>
              <th class="left">Start Time</th>
              <th class="left">Completion Time</th>
            </tr>
          </thead>
          <tbody>
            @for (t of tasks.tasks(); track t.id) {
              <tr>
                <td class="left">{{ t.name }}</td>
                <td class="left">{{ t.target }}</td>
                <td class="left">
                  @switch (t.status) {
                    @case ('running') {
                      <clr-spinner clrInline></clr-spinner> In progress
                    }
                    @case ('ok') {
                      <cds-icon shape="success-standard" status="success" solid size="14"></cds-icon> Completed
                    }
                    @case ('error') {
                      <cds-icon shape="error-standard" status="danger" solid size="14"></cds-icon> Failed
                    }
                  }
                </td>
                <td class="left">{{ t.details }}</td>
                <td class="left">{{ t.initiator }}</td>
                <td class="left">{{ t.start | date: 'MM/dd/yyyy, h:mm:ss a' }}</td>
                <td class="left">{{ t.end | date: 'MM/dd/yyyy, h:mm:ss a' }}</td>
              </tr>
            } @empty {
              <tr>
                <td colspan="7" class="fc-muted">No recent tasks</td>
              </tr>
            }
          </tbody>
        </table>
      </div>
    }
  `,
  styles: `
    :host {
      display: block;
      flex: 0 0 auto;
      border-top: 1px solid var(--fc-border);
      background: #fff;
    }
    .bar {
      display: flex;
      align-items: center;
      gap: 0.4rem;
      width: 100%;
      height: 1.5rem;
      padding: 0 0.6rem;
      border: 0;
      background: none;
      cursor: pointer;
    }
    .tab {
      font-size: 0.65rem;
      font-weight: 600;
      line-height: 1.5rem;
      border-bottom: 3px solid #0072a3;
    }
    .body {
      height: 11rem;
      overflow: auto;
      border-top: 1px solid var(--fc-border);
    }
    .table {
      margin: 0;
      border: 0;
      border-radius: 0;
    }
    .table th {
      position: sticky;
      top: 0;
      background: #fafafa;
    }
  `,
})
export class RecentTasksComponent {
  protected readonly tasks = inject(TaskService);
  protected readonly running = computed(() => this.tasks.tasks().filter((t) => t.status === 'running').length);
}
