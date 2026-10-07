import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ActionsService, ApiService, DialogService, InventoryService, Snapshot, Vm, errorMessage } from '../../core';

interface SnapshotNode {
  snapshot: Snapshot;
  depth: number;
}

/** The Snapshots tab: snapshot tree on the left, details of the selection on the right. */
@Component({
  selector: 'app-vm-snapshots',
  imports: [ClarityModule],
  template: `
    <div class="fc-toolbar">
      <button class="btn btn-sm btn-outline" (click)="dialogs.takeSnapshot.set(vm())">Take Snapshot...</button>
      <button class="btn btn-sm btn-outline" [disabled]="!selected()" (click)="revert()">Revert</button>
      <button class="btn btn-sm btn-outline" [disabled]="!selected()" (click)="remove()">Delete</button>
      <button class="btn btn-sm btn-outline" [disabled]="nodes().length === 0" (click)="actions.removeAllSnapshots(vm())">Delete All</button>
    </div>

    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    }

    <div class="split">
      <div class="tree card">
        @if (loading() && nodes().length === 0) {
          <div class="fc-empty"><clr-spinner clrMedium></clr-spinner></div>
        } @else if (nodes().length === 0) {
          <div class="fc-empty">No snapshots available</div>
        } @else {
          <ul>
            @for (n of nodes(); track n.snapshot.id) {
              <li [style.padding-left.rem]="0.4 + n.depth * 0.9">
                <button type="button" class="fc-link node" [class.active]="selectedId() === n.snapshot.id" (click)="selectedId.set(n.snapshot.id)">
                  <cds-icon shape="camera" size="14"></cds-icon> {{ n.snapshot.name }}
                </button>
              </li>
            }
            <li class="here" [style.padding-left.rem]="0.4 + depthOfHere() * 0.9">
              <cds-icon shape="map-marker" size="14"></cds-icon> You are here
            </li>
          </ul>
        }
      </div>
      <div class="details card">
        @if (selected(); as s) {
          <div class="card-block">
            <dl class="fc-props">
              <dt>Name</dt>
              <dd>{{ s.name }}</dd>
              <dt>Description</dt>
              <dd>{{ s.description || '--' }}</dd>
              <dt>Timestamp</dt>
              <dd>{{ s.create_time || '--' }}</dd>
              <dt>Snapshot ID</dt>
              <dd>{{ s.id }}</dd>
            </dl>
          </div>
        } @else {
          <div class="fc-empty">Select a snapshot to see its details</div>
        }
      </div>
    </div>
  `,
  styles: `
    .split {
      display: grid;
      grid-template-columns: minmax(240px, 1fr) minmax(280px, 1.4fr);
      gap: 0.6rem;
    }
    .card {
      margin: 0;
      min-height: 14rem;
    }
    ul {
      list-style: none;
      margin: 0;
      padding: 0.3rem 0;
    }
    li {
      padding: 0.1rem 0.4rem;
      font-size: 0.65rem;
      white-space: nowrap;
    }
    .node {
      padding: 0.1rem 0.3rem;
      color: inherit;
    }
    .node:hover {
      background: #f1f6f8;
    }
    .node.active {
      background: #d8e3e9;
    }
    li.here {
      padding-top: 0.2rem;
      color: #6c6c6c;
    }
  `,
})
export class VmSnapshotsComponent {
  private readonly api = inject(ApiService);
  private readonly inventory = inject(InventoryService);
  protected readonly actions = inject(ActionsService);
  protected readonly dialogs = inject(DialogService);

  readonly vm = input.required<Vm>();

  // Primitive ids, so a refreshed copy of the same VM does not trigger another load.
  private readonly hostId = computed(() => this.vm().host_id);
  private readonly vmId = computed(() => this.vm().id);

  protected readonly snapshots = signal<Snapshot[]>([]);
  protected readonly selectedId = signal<string | null>(null);
  protected readonly loading = signal(false);
  protected readonly error = signal('');

  /** The API returns a flat list; nesting is derived from the "parent/child" snapshot path. */
  protected readonly nodes = computed<SnapshotNode[]>(() =>
    this.snapshots().map((snapshot) => ({ snapshot, depth: Math.max(0, (snapshot.path || '').split('/').filter(Boolean).length - 1) })),
  );
  protected readonly selected = computed(() => this.snapshots().find((s) => s.id === this.selectedId()) ?? null);
  protected readonly depthOfHere = computed(() => Math.max(0, ...this.nodes().map((n) => n.depth + 1)));

  constructor() {
    // Reload when another VM is shown and after every inventory refresh (which follows each task).
    effect(() => {
      const hid = this.hostId();
      const vmid = this.vmId();
      this.inventory.revision();
      untracked(() => void this.load(hid, vmid));
    });
  }

  private async load(hid: string, vmid: string): Promise<void> {
    this.loading.set(true);
    try {
      const list = await this.api.snapshots(hid, vmid);
      if (this.vm().host_id !== hid || this.vm().id !== vmid) return;
      this.snapshots.set(list);
      this.error.set('');
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.loading.set(false);
    }
  }

  protected async revert(): Promise<void> {
    const s = this.selected();
    const vm = this.vm();
    if (!s) return;
    const ok = await this.dialogs.confirm({
      title: 'Revert to Snapshot',
      message: `Revert ${vm.name} to "${s.name}"? The current state of the virtual machine is lost unless it was saved in a snapshot.`,
      okLabel: 'Revert',
      danger: true,
    });
    if (ok) await this.actions.track('Revert to snapshot', vm.name, () => this.api.revertSnapshot(vm.host_id, vm.id, s.id));
  }

  protected async remove(): Promise<void> {
    const s = this.selected();
    const vm = this.vm();
    if (!s) return;
    const ok = await this.dialogs.confirm({
      title: 'Delete Snapshot',
      message: `Delete snapshot "${s.name}" of ${vm.name}?`,
      okLabel: 'Delete',
      danger: true,
    });
    if (ok) await this.actions.track('Remove snapshot', vm.name, () => this.api.deleteSnapshot(vm.host_id, vm.id, s.id));
  }
}
