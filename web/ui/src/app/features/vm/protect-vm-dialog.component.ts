import { Component, OnInit, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import {
  ActionsService,
  ApiService,
  BackupRequest,
  BytesPipe,
  DialogService,
  InventoryService,
  Schedule,
  ScheduleFrequency,
  ScheduleKind,
  TaskService,
  Vm,
  errorMessage,
} from '../../core';

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

/**
 * Protect a virtual machine: back it up now, or define a schedule for backups
 * or snapshots. Also used to edit an existing schedule.
 */
@Component({
  selector: 'app-protect-vm-dialog',
  imports: [ClarityModule, FormsModule, BytesPipe],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="lg">
      <h3 class="modal-title">
        {{ schedule() ? (edit() ? 'Edit Scheduled Task' : 'Schedule Backup or Snapshot') : 'Back Up Now' }}
        <span class="fc-muted">| {{ vmName() }}</span>
      </h3>
      <div class="modal-body">
        @if (error()) {
          <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
            <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
          </clr-alert>
        }
        <form clrForm clrLayout="horizontal">
          @if (schedule()) {
            <clr-radio-container clrInline>
              <label>Task</label>
              <clr-radio-wrapper>
                <input type="radio" clrRadio name="kind" value="backup" [(ngModel)]="kind" />
                <label>Backup</label>
              </clr-radio-wrapper>
              <clr-radio-wrapper>
                <input type="radio" clrRadio name="kind" value="snapshot" [(ngModel)]="kind" />
                <label>Snapshot</label>
              </clr-radio-wrapper>
            </clr-radio-container>
            <clr-select-container>
              <label>Run</label>
              <select clrSelect name="frequency" [(ngModel)]="frequency">
                <option value="hourly">Every hour</option>
                <option value="daily">Every day</option>
                <option value="weekly">Every week</option>
              </select>
            </clr-select-container>
            @if (frequency === 'weekly') {
              <clr-select-container>
                <label>On</label>
                <select clrSelect name="weekday" [(ngModel)]="weekday">
                  @for (d of weekdays; track $index) {
                    <option [ngValue]="$index">{{ d }}</option>
                  }
                </select>
              </clr-select-container>
            }
            <clr-input-container>
              <label>{{ frequency === 'hourly' ? 'At minute' : 'At' }}</label>
              @if (frequency === 'hourly') {
                <input clrInput type="number" name="minute" min="0" max="59" [(ngModel)]="minute" />
              } @else {
                <input clrInput type="time" name="time" [(ngModel)]="time" />
              }
              <clr-control-helper>{{ frequency === 'hourly' ? 'Minute of each hour' : 'Time zone: ' + timezone }}</clr-control-helper>
            </clr-input-container>
          }

          @if (kind === 'backup') {
            <clr-select-container>
              <label>Datastore</label>
              <select clrSelect name="datastore" [(ngModel)]="datastore">
                @for (d of datastores(); track d.name) {
                  <option [value]="d.name">{{ d.name }} ({{ d.free_bytes | bytes }} free)</option>
                }
              </select>
              <clr-control-helper>On {{ hostName() }}, in the hostwarden-backups folder</clr-control-helper>
            </clr-select-container>
            @if (otherHosts().length > 0) {
              <clr-select-container>
                <label>Then move to</label>
                <select clrSelect name="targetHost" [ngModel]="targetHost()" (ngModelChange)="selectTargetHost($event)">
                  <option value="">Keep it on {{ hostName() }}</option>
                  @for (h of otherHosts(); track h.id) {
                    <option [value]="h.id">{{ h.name }}</option>
                  }
                </select>
                <clr-control-helper>A copy on another host survives the loss of this one</clr-control-helper>
              </clr-select-container>
              @if (targetHost()) {
                <clr-select-container>
                  <label>Datastore on {{ inventory.hostName(targetHost()) }}</label>
                  <select clrSelect name="targetDatastore" [(ngModel)]="targetDatastore">
                    @for (d of targetDatastores(); track d.name) {
                      <option [value]="d.name">{{ d.name }} ({{ d.free_bytes | bytes }} free)</option>
                    }
                  </select>
                </clr-select-container>
              }
            }
          }
          <clr-input-container>
            <label>Keep</label>
            <input clrInput type="number" name="keep" min="0" max="365" [(ngModel)]="keep" />
            <clr-control-helper>
              {{ kind === 'backup' ? 'Backups of this virtual machine to keep at the destination' : 'Scheduled snapshots to keep' }} (0 = all)
            </clr-control-helper>
          </clr-input-container>
        </form>
        <p class="fc-muted">
          @if (kind === 'backup') {
            A running virtual machine is not stopped: a temporary snapshot is taken, the disks are copied (thin) and the snapshot is removed. The backup
            can be restored as a new virtual machine from Hostwarden &gt; Backups.
          } @else {
            Snapshots are taken without memory. Only the snapshots created by schedules (named fc-auto-...) are removed when the limit is exceeded.
            Snapshots are not backups: they live on the same disks as the virtual machine.
          }
        </p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" (click)="close()">Cancel</button>
        <button type="button" class="btn btn-primary" [clrLoading]="busy()" [disabled]="!valid()" (click)="submit()">
          {{ schedule() ? 'Save' : 'Back Up' }}
        </button>
      </div>
    </clr-modal>
  `,
})
export class ProtectVmDialogComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly actions = inject(ActionsService);
  private readonly dialogs = inject(DialogService);
  protected readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);
  protected readonly weekdays = WEEKDAYS;

  /** The VM to protect; unset when editing a schedule from the list. */
  readonly vm = input<Vm>();
  /** False = back up now, true = define a schedule. */
  readonly schedule = input(false);
  /** Existing schedule being edited. */
  readonly edit = input<Schedule>();

  private readonly hostId = computed(() => this.vm()?.host_id ?? this.edit()?.host_id ?? '');
  protected readonly hostName = computed(() => this.inventory.hostName(this.hostId()));
  protected readonly vmName = computed(() => this.vm()?.name ?? this.edit()?.vm_name ?? '');
  protected readonly datastores = computed(() => (this.inventory.datastoresByHost()[this.hostId()] ?? []).filter((d) => d.accessible));
  protected readonly otherHosts = computed(() => this.inventory.hosts().filter((h) => h.id !== this.hostId() && h.status?.connected));
  protected readonly targetHost = signal('');
  protected readonly targetDatastores = computed(() => (this.inventory.datastoresByHost()[this.targetHost()] ?? []).filter((d) => d.accessible));
  protected readonly error = signal('');
  protected readonly busy = signal(false);

  protected kind: ScheduleKind = 'backup';
  protected frequency: ScheduleFrequency = 'daily';
  protected time = '02:00';
  protected minute = 0;
  protected weekday = 0;
  protected timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  protected datastore = '';
  protected targetDatastore = '';
  protected keep = 7;

  ngOnInit(): void {
    const largest = (list: { name: string; free_bytes: number }[]) => [...list].sort((a, b) => b.free_bytes - a.free_bytes)[0]?.name ?? '';
    this.datastore = largest(this.datastores());
    const e = this.edit();
    if (!e) return;
    this.kind = e.kind;
    this.frequency = e.frequency;
    this.time = `${String(e.hour).padStart(2, '0')}:${String(e.minute).padStart(2, '0')}`;
    this.minute = e.minute;
    this.weekday = e.weekday;
    this.timezone = e.timezone || this.timezone;
    this.keep = e.keep;
    if (e.backup) {
      this.datastore = e.backup.datastore;
      this.targetHost.set(e.backup.target_host_id ?? '');
      this.targetDatastore = e.backup.target_datastore ?? '';
    }
  }

  protected selectTargetHost(id: string): void {
    this.targetHost.set(id);
    this.targetDatastore = [...this.targetDatastores()].sort((a, b) => b.free_bytes - a.free_bytes)[0]?.name ?? '';
  }

  protected valid(): boolean {
    if (!(this.keep >= 0 && this.keep <= 365)) return false;
    if (this.kind === 'backup' && (!this.datastore || (!!this.targetHost() && !this.targetDatastore))) return false;
    if (!this.schedule()) return true;
    return this.frequency === 'hourly' ? this.minute >= 0 && this.minute <= 59 : /^\d{2}:\d{2}$/.test(this.time);
  }

  protected close(): void {
    this.dialogs.protectVm.set(null);
  }

  private backupRequest(): BackupRequest {
    return {
      datastore: this.datastore,
      keep: Number(this.keep),
      ...(this.targetHost() ? { target_host_id: this.targetHost(), target_datastore: this.targetDatastore } : {}),
    };
  }

  protected async submit(): Promise<void> {
    if (!this.valid() || this.busy()) return;
    const vm = this.vm();
    if (!this.schedule()) {
      if (!vm) return;
      this.tasks.panelOpen.set(true);
      // Followed in Recent Tasks; the dialog does not need to stay open.
      void this.actions.backupVm(vm, this.backupRequest());
      this.close();
      return;
    }
    const e = this.edit();
    const [hour, minute] = this.frequency === 'hourly' ? [0, Number(this.minute)] : this.time.split(':').map(Number);
    const sched: Schedule = {
      ...(e?.id ? { id: e.id } : {}),
      host_id: this.hostId(),
      vm_id: vm?.id ?? e?.vm_id ?? '',
      kind: this.kind,
      frequency: this.frequency,
      hour,
      minute,
      weekday: Number(this.weekday),
      timezone: this.timezone,
      keep: Number(this.keep),
      enabled: e?.enabled ?? true,
      ...(this.kind === 'backup' ? { backup: this.backupRequest() } : {}),
    };
    this.error.set('');
    this.busy.set(true);
    try {
      await this.tasks.run(e ? 'Update scheduled task' : 'Create scheduled task', this.vmName(), () => this.api.saveSchedule(sched));
      this.close();
    } catch (err) {
      this.error.set(errorMessage(err));
    } finally {
      this.busy.set(false);
    }
  }
}
