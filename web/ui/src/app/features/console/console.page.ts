import { Component, OnDestroy, computed, effect, inject, input } from '@angular/core';

import { InventoryService, TaskService } from '../../core';
import { ConfirmDialogComponent } from '../../shell/confirm-dialog.component';
import { ConsoleViewComponent } from './console-view.component';

/** Web console of a virtual machine in its own browser tab, without the shell around it. */
@Component({
  selector: 'app-console-page',
  imports: [ConsoleViewComponent, ConfirmDialogComponent],
  template: `
    <app-console-view [hid]="hid()" [vmid]="vmid()" />
    <!-- This tab has no shell: host the confirmation dialog (Power Off) here. -->
    <app-confirm-dialog />
  `,
  styles: `
    :host {
      display: block;
      height: 100vh;
    }
  `,
})
export class ConsolePage implements OnDestroy {
  private readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);

  readonly hid = input.required<string>();
  readonly vmid = input.required<string>();

  private readonly vmName = computed(() => this.inventory.vmsOf(this.hid()).find((v) => v.id === this.vmid())?.name ?? `VM ${this.vmid()}`);

  constructor() {
    // This tab has no shell, so it keeps its own inventory (VM name and power state).
    this.inventory.start();
    this.tasks.start();
    effect(() => (document.title = `${this.vmName()} - Console`));
  }

  ngOnDestroy(): void {
    this.inventory.stop();
    this.tasks.stop();
  }
}
