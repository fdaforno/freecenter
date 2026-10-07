import { DatePipe } from '@angular/common';
import { Component, OnDestroy, effect, inject, input, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { ActionsService, ApiService, Backup, BytesPipe, DialogService, InventoryService, Schedule, TaskService, errorMessage } from '../../core';

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const REFRESH_MS = 15000;

/** The Backups and Scheduled Tasks tabs of the inventory root. */
@Component({
  selector: 'app-protection',
  imports: [ClarityModule, FormsModule, RouterLink, DatePipe, BytesPipe],
  template: `
    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    }

    @if (view() === 'backups') {
      <table class="table table-compact">
        <thead>
          <tr>
            <th class="left">Virtual Machine</th>
            <th class="left">Taken</th>
            <th class="left">Size</th>
            <th class="left">Host</th>
            <th class="left">Datastore</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          @for (b of backups(); track b.host_id + b.datastore + b.vm_name + b.stamp) {
            <tr>
              <td class="left">{{ b.vm_name }}</td>
              <td class="left">{{ b.created | date: 'MM/dd/yyyy, h:mm:ss a' }}</td>
              <td class="left">{{ b.size_bytes | bytes }}</td>
              <td class="left"><a [routerLink]="['/host', b.host_id]">{{ inventory.hostName(b.host_id) }}</a></td>
              <td class="left">{{ b.datastore }}</td>
              <td class="right">
                <button class="btn btn-sm btn-link" (click)="openRestore(b)">Restore...</button>
                <button class="btn btn-sm btn-link" (click)="removeBackup(b)">Delete</button>
              </td>
            </tr>
          } @empty {
            <tr>
              <td colspan="6" class="fc-muted">{{ loaded() ? 'No backups yet. Use Actions > Backup on a virtual machine.' : 'Loading...' }}</td>
            </tr>
          }
        </tbody>
      </table>
      <p class="fc-muted">Backups are folders in freecenter-backups on each datastore. Restoring creates a new virtual machine and leaves the backup in place.</p>
    } @else {
      <table class="table table-compact">
        <thead>
          <tr>
            <th class="left">Virtual Machine</th>
            <th class="left">Task</th>
            <th class="left">Schedule</th>
            <th class="left">Keep</th>
            <th class="left">Next Run</th>
            <th class="left">Last Run</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          @for (s of schedules(); track s.id) {
            <tr>
              <td class="left"><a [routerLink]="['/vm', s.host_id, s.vm_id]">{{ s.vm_name }}</a></td>
              <td class="left">{{ taskText(s) }}</td>
              <td class="left">{{ whenText(s) }}</td>
              <td class="left">{{ s.keep || 'All' }}</td>
              <td class="left">{{ s.enabled ? (s.next_run | date: 'MM/dd/yyyy, h:mm a') : 'Disabled' }}</td>
              <td class="left" [title]="s.last_error ?? ''">
                @if (s.last_run) {
                  <cds-icon [shape]="s.last_status === 'ok' ? 'success-standard' : 'error-standard'" [status]="s.last_status === 'ok' ? 'success' : 'danger'" solid size="14"></cds-icon>
                  {{ s.last_run | date: 'MM/dd/yyyy, h:mm a' }}
                  @if (s.last_status !== 'ok') {
                    - {{ s.last_error }}
                  }
                } @else {
                  Never
                }
              </td>
              <td class="right">
                <button class="btn btn-sm btn-link" (click)="runNow(s)">Run Now</button>
                <button class="btn btn-sm btn-link" (click)="dialogs.protectVm.set({ schedule: true, edit: s })">Edit...</button>
                <button class="btn btn-sm btn-link" (click)="toggle(s)">{{ s.enabled ? 'Disable' : 'Enable' }}</button>
                <button class="btn btn-sm btn-link" (click)="removeSchedule(s)">Delete</button>
              </td>
            </tr>
          } @empty {
            <tr>
              <td colspan="7" class="fc-muted">{{ loaded() ? 'No scheduled tasks. Use Actions > Backup > Schedule on a virtual machine.' : 'Loading...' }}</td>
            </tr>
          }
        </tbody>
      </table>
    }

    @if (restore(); as r) {
      <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="restore.set(null)" [clrModalStaticBackdrop]="true" clrModalSize="md">
        <h3 class="modal-title">Restore from Backup <span class="fc-muted">| {{ r.backup.vm_name }}</span></h3>
        <div class="modal-body">
          <form clrForm clrLayout="horizontal" (ngSubmit)="doRestore()">
            <clr-input-container>
              <label>New virtual machine name</label>
              <input clrInput name="name" [(ngModel)]="r.name" size="40" maxlength="80" />
            </clr-input-container>
            <clr-select-container>
              <label>Datastore</label>
              <select clrSelect name="datastore" [(ngModel)]="r.datastore">
                @for (d of inventory.datastoresOf(r.backup.host_id); track d.name) {
                  <option [value]="d.name">{{ d.name }} ({{ d.free_bytes | bytes }} free)</option>
                }
              </select>
            </clr-select-container>
            <clr-checkbox-container>
              <clr-checkbox-wrapper>
                <input type="checkbox" clrCheckbox name="powerOn" [(ngModel)]="r.powerOn" />
                <label>Power on after restoring</label>
              </clr-checkbox-wrapper>
            </clr-checkbox-container>
          </form>
          <p class="fc-muted">
            Backup of {{ r.backup.created | date: 'medium' }}. The new virtual machine is created on {{ inventory.hostName(r.backup.host_id) }} with a new
            identity (UUID, MAC addresses), so it can run next to the original; if it replaces the original, delete that first or keep one of them off
            the network.
          </p>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-outline" (click)="restore.set(null)">Cancel</button>
          <button type="button" class="btn btn-primary" [disabled]="!validName(r.name) || !r.datastore" (click)="doRestore()">Restore</button>
        </div>
      </clr-modal>
    }
  `,
  styles: `
    .table {
      margin-top: 0;
    }
    .right {
      text-align: right;
      white-space: nowrap;
    }
    td .btn {
      margin: 0;
    }
  `,
})
export class ProtectionComponent implements OnDestroy {
  private readonly api = inject(ApiService);
  private readonly actions = inject(ActionsService);
  private readonly tasks = inject(TaskService);
  protected readonly dialogs = inject(DialogService);
  protected readonly inventory = inject(InventoryService);
  private timer: ReturnType<typeof setInterval> | null = null;

  readonly view = input.required<'backups' | 'schedules'>();

  protected readonly backups = signal<Backup[]>([]);
  protected readonly schedules = signal<Schedule[]>([]);
  protected readonly loaded = signal(false);
  protected readonly error = signal('');
  protected readonly restore = signal<{ backup: Backup; name: string; datastore: string; powerOn: boolean } | null>(null);

  constructor() {
    effect(() => {
      this.view();
      // A schedule saved in the dialog shows up as soon as the dialog closes.
      this.dialogs.protectVm();
      untracked(() => void this.load());
    });
    this.timer = setInterval(() => {
      if (!document.hidden) void this.load();
    }, REFRESH_MS);
  }

  ngOnDestroy(): void {
    if (this.timer) clearInterval(this.timer);
  }

  private async load(): Promise<void> {
    const view = this.view();
    try {
      if (view === 'backups') this.backups.set(await this.api.backups());
      else this.schedules.set(await this.api.schedules());
      this.error.set('');
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.loaded.set(true);
    }
  }

  protected taskText(s: Schedule): string {
    if (s.kind === 'snapshot') return 'Snapshot';
    const b = s.backup;
    if (!b) return 'Backup';
    return b.target_host_id ? `Backup to ${this.inventory.hostName(b.target_host_id)} / ${b.target_datastore}` : `Backup to ${b.datastore}`;
  }

  protected whenText(s: Schedule): string {
    const mm = String(s.minute).padStart(2, '0');
    if (s.frequency === 'hourly') return `Every hour at :${mm}`;
    const time = `${String(s.hour).padStart(2, '0')}:${mm} (${s.timezone})`;
    return s.frequency === 'weekly' ? `Every ${WEEKDAYS[s.weekday]} at ${time}` : `Every day at ${time}`;
  }

  protected validName(name: string): boolean {
    const n = name.trim();
    return !!n && !/["'/\\]/.test(n);
  }

  protected openRestore(b: Backup): void {
    this.restore.set({ backup: b, name: `${b.vm_name}-restored`, datastore: b.datastore, powerOn: false });
  }

  protected doRestore(): void {
    const r = this.restore();
    if (!r || !this.validName(r.name)) return;
    this.tasks.panelOpen.set(true);
    void this.actions.restoreBackup(r.backup, r.name.trim(), r.datastore, r.powerOn);
    this.restore.set(null);
  }

  protected async removeBackup(b: Backup): Promise<void> {
    const ok = await this.dialogs.confirm({
      title: 'Delete Backup',
      message: `Delete the backup of ${b.vm_name} taken on ${new Date(b.created).toLocaleString('en-US')}? This cannot be undone.`,
      okLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    await this.actions.track('Delete backup', b.vm_name, () => this.api.deleteBackup(b));
    await this.load();
  }

  protected async runNow(s: Schedule): Promise<void> {
    if (!s.id) return;
    const id = s.id;
    this.tasks.panelOpen.set(true);
    // The run is its own task ("Scheduled backup"); this request only starts it.
    try {
      await this.api.runSchedule(id);
      void this.tasks.refresh();
    } catch (e) {
      this.error.set(errorMessage(e));
    }
  }

  protected async toggle(s: Schedule): Promise<void> {
    await this.actions.track(s.enabled ? 'Disable scheduled task' : 'Enable scheduled task', s.vm_name ?? '', () => this.api.saveSchedule({ ...s, enabled: !s.enabled }));
    await this.load();
  }

  protected async removeSchedule(s: Schedule): Promise<void> {
    if (!s.id) return;
    const id = s.id;
    const ok = await this.dialogs.confirm({ title: 'Delete Scheduled Task', message: `Delete this scheduled task of ${s.vm_name}? Existing backups and snapshots are kept.`, okLabel: 'Delete', danger: true });
    if (!ok) return;
    await this.actions.track('Delete scheduled task', s.vm_name ?? '', () => this.api.deleteSchedule(id));
    await this.load();
  }
}
