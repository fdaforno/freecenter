import { Component, OnInit, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import {
  ActionsService,
  ApiService,
  BytesPipe,
  DialogService,
  InventoryService,
  MigrationCheck,
  MigrationMode,
  TaskService,
  Vm,
  errorMessage,
} from '../../core';

/**
 * Migrate a virtual machine to another host. The migration is not live: the
 * VM is stopped or suspended, copied host-to-host and started on the target.
 * The dialog runs the compatibility checks and lets the user pick how a
 * running VM is handled.
 */
@Component({
  selector: 'app-migrate-vm-dialog',
  imports: [ClarityModule, FormsModule, BytesPipe],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="lg">
      <h3 class="modal-title">Migrate <span class="fc-muted">| {{ vm().name }}</span></h3>
      <div class="modal-body">
        @if (targets().length === 0) {
          <clr-alert clrAlertType="info" [clrAlertClosable]="false">
            <clr-alert-item><span class="alert-text">There is no other connected host to migrate to. Add a second ESXi host first.</span></clr-alert-item>
          </clr-alert>
        } @else {
          <form clrForm clrLayout="horizontal">
            <clr-select-container>
              <label>Target host</label>
              <select clrSelect name="host" [ngModel]="hostId()" (ngModelChange)="selectHost($event)">
                @for (h of targets(); track h.id) {
                  <option [value]="h.id">{{ h.name }} ({{ h.address }})</option>
                }
              </select>
            </clr-select-container>
            <clr-select-container>
              <label>Target datastore</label>
              <select clrSelect name="datastore" [ngModel]="datastore()" (ngModelChange)="selectDatastore($event)">
                @for (d of datastores(); track d.name) {
                  <option [value]="d.name">{{ d.name }} ({{ d.free_bytes | bytes }} free)</option>
                }
              </select>
            </clr-select-container>
          </form>

          <h5>Compatibility</h5>
          @if (checking()) {
            <p><clr-spinner clrInline></clr-spinner> Running compatibility checks...</p>
          } @else if (error()) {
            <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
              <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
            </clr-alert>
          } @else if (check(); as c) {
            @if (c.blockers.length > 0) {
              <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
                @for (b of c.blockers; track b) {
                  <clr-alert-item><span class="alert-text">{{ b }}</span></clr-alert-item>
                }
              </clr-alert>
            } @else {
              <clr-alert clrAlertType="success" [clrAlertClosable]="false">
                <clr-alert-item>
                  <span class="alert-text">
                    Compatibility checks succeeded.
                    @if (c.shared) {
                      The datastore is shared by both hosts: no files are copied.
                    } @else {
                      {{ c.size_bytes | bytes }} to copy directly between the hosts.
                    }
                  </span>
                </clr-alert-item>
              </clr-alert>
            }
            @if (c.warnings.length > 0) {
              <clr-alert clrAlertType="warning" [clrAlertClosable]="false">
                @for (w of c.warnings; track w) {
                  <clr-alert-item><span class="alert-text">{{ w }}</span></clr-alert-item>
                }
              </clr-alert>
            }

            @if (c.blockers.length === 0) {
              @if (c.power_state === 'poweredOn') {
                @if (c.cpu_compatible) {
                  <h5>Running virtual machine</h5>
                  <clr-radio-container>
                    <clr-radio-wrapper>
                      <input type="radio" clrRadio name="mode" value="suspend" [(ngModel)]="runningMode" />
                      <label>Suspend, migrate and resume (the guest keeps running where it left off)</label>
                    </clr-radio-wrapper>
                    <clr-radio-wrapper>
                      <input type="radio" clrRadio name="mode" value="stop_start" [(ngModel)]="runningMode" />
                      <label>Shut down, migrate and power on (the guest operating system restarts)</label>
                    </clr-radio-wrapper>
                  </clr-radio-container>
                } @else {
                  <clr-alert clrAlertType="warning" [clrAlertClosable]="false">
                    <clr-alert-item>
                      <span class="alert-text">
                        The hosts have different CPUs ({{ c.source_cpu }} → {{ c.target_cpu }}). A running virtual machine cannot be moved between them
                        without stopping it. FreeCenter can shut it down, migrate it and start it again on the target host.
                      </span>
                    </clr-alert-item>
                  </clr-alert>
                  <clr-checkbox-wrapper>
                    <input type="checkbox" clrCheckbox name="confirmStop" [(ngModel)]="confirmStop" />
                    <label>Shut down {{ vm().name }}, migrate it and start it on the target host</label>
                  </clr-checkbox-wrapper>
                }
                @if (!c.shared) {
                  <clr-checkbox-wrapper>
                    <input type="checkbox" clrCheckbox name="precopy" [(ngModel)]="precopy" [disabled]="hasSnapshots()" />
                    <label>
                      Copy the disks while the virtual machine is still running (shorter downtime)
                      @if (hasSnapshots()) {
                        - not available: the virtual machine has snapshots
                      }
                    </label>
                  </clr-checkbox-wrapper>
                }
                <p class="fc-muted">
                  @if (c.shared) {
                    The virtual machine is unavailable only while it is stopped on one host and started on the other.
                  } @else if (usesPrecopy(c)) {
                    A temporary snapshot freezes the disks, which are cloned (this needs free space on the source datastore) and copied with the virtual
                    machine running; it is then stopped only to copy the changes made in the meantime, and the snapshot is removed on the target.
                  } @else {
                    The virtual machine is unavailable while all its files are copied.
                  }
                  The guest is shut down through VMware Tools; if it does not respond within 3 minutes it is powered off.
                </p>
              } @else {
                <p class="fc-muted">The virtual machine is not running: its files are copied and it is registered on the target host.</p>
              }

              @if (!c.shared) {
                <h5>Options</h5>
                <clr-checkbox-container>
                  <clr-checkbox-wrapper>
                    <input type="checkbox" clrCheckbox name="deleteSource" [(ngModel)]="deleteSource" />
                    <label>Delete the source files after a successful migration</label>
                  </clr-checkbox-wrapper>
                  <clr-checkbox-wrapper>
                    <input type="checkbox" clrCheckbox name="reclaimThin" [(ngModel)]="reclaimThin" />
                    <label>Keep thin disks thin on the target (takes longer; otherwise they use their full {{ c.provisioned_gb }} GB)</label>
                  </clr-checkbox-wrapper>
                </clr-checkbox-container>
              }
              <p class="fc-muted">
                The source virtual machine is removed only after the target is registered and, if it was running, powered on. On any failure the source is
                restored.
              </p>
            }
          }
        }
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" (click)="close()">Cancel</button>
        <button type="button" class="btn btn-primary" [disabled]="!canMigrate()" (click)="migrate()">Migrate</button>
      </div>
    </clr-modal>
  `,
  styles: `
    h5 {
      margin-top: 0.9rem;
      font-weight: 600;
    }
    clr-alert {
      display: block;
      margin-top: 0.3rem;
    }
  `,
})
export class MigrateVmDialogComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly actions = inject(ActionsService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);

  readonly vm = input.required<Vm>();

  protected readonly targets = computed(() => this.inventory.hosts().filter((h) => h.id !== this.vm().host_id && h.status?.connected));
  protected readonly hostId = signal('');
  protected readonly datastore = signal('');
  protected readonly datastores = computed(() => (this.inventory.datastoresByHost()[this.hostId()] ?? []).filter((d) => d.accessible));
  protected readonly check = signal<MigrationCheck | null>(null);
  protected readonly checking = signal(false);
  protected readonly error = signal('');

  protected runningMode: MigrationMode = 'suspend';
  protected confirmStop = false;
  protected deleteSource = true;
  protected reclaimThin = true;
  protected precopy = true;

  /** Discards the result of a check that was superseded by a newer selection. */
  private checkSeq = 0;

  ngOnInit(): void {
    const first = this.targets()[0];
    if (first) this.selectHost(first.id);
  }

  protected close(): void {
    this.dialogs.migrateVm.set(null);
  }

  protected selectHost(id: string): void {
    this.hostId.set(id);
    // Default to the datastore with the most free space.
    const best = [...this.datastores()].sort((a, b) => b.free_bytes - a.free_bytes)[0];
    this.selectDatastore(best?.name ?? '');
  }

  protected selectDatastore(name: string): void {
    this.datastore.set(name);
    void this.runCheck();
  }

  private async runCheck(): Promise<void> {
    const seq = ++this.checkSeq;
    this.check.set(null);
    this.error.set('');
    this.confirmStop = false;
    if (!this.hostId() || !this.datastore()) return;
    this.checking.set(true);
    try {
      const res = await this.api.migrateCheck(this.vm().host_id, this.vm().id, { target_host_id: this.hostId(), target_datastore: this.datastore() });
      if (seq !== this.checkSeq) return;
      this.check.set(res);
      this.runningMode = res.cpu_compatible ? 'suspend' : 'stop_start';
    } catch (e) {
      if (seq === this.checkSeq) this.error.set(errorMessage(e));
    } finally {
      if (seq === this.checkSeq) this.checking.set(false);
    }
  }

  private mode(c: MigrationCheck): MigrationMode {
    if (c.power_state !== 'poweredOn') return 'cold';
    return c.cpu_compatible ? this.runningMode : 'stop_start';
  }

  protected hasSnapshots(): boolean {
    return this.vm().snapshot_count > 0;
  }

  protected usesPrecopy(c: MigrationCheck): boolean {
    return this.precopy && c.power_state === 'poweredOn' && !c.shared && !this.hasSnapshots();
  }

  protected canMigrate(): boolean {
    const c = this.check();
    if (!c || this.checking() || c.blockers.length > 0) return false;
    // Different CPUs: stopping the running VM needs the explicit confirmation.
    return c.power_state !== 'poweredOn' || c.cpu_compatible || this.confirmStop;
  }

  protected migrate(): void {
    const c = this.check();
    if (!c || !this.canMigrate()) return;
    const target = this.inventory.hostName(this.hostId());
    this.tasks.panelOpen.set(true);
    // Followed in Recent Tasks; the dialog does not need to stay open.
    void this.actions.migrateVm(
      this.vm(),
      {
        target_host_id: this.hostId(),
        target_datastore: this.datastore(),
        mode: this.mode(c),
        delete_source: this.deleteSource,
        reclaim_thin: this.reclaimThin,
        precopy: this.usesPrecopy(c),
      },
      target,
    );
    this.close();
  }
}
