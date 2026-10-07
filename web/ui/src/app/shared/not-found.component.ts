import { Component, inject, input } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { InventoryService } from '../core';

/** Placeholder shown while the inventory loads or when the routed object does not exist. */
@Component({
  selector: 'app-not-found',
  imports: [ClarityModule],
  template: `
    <div class="fc-empty">
      @if (!inventory.loaded()) {
        <clr-spinner clrMedium></clr-spinner>
      } @else {
        <cds-icon shape="exclamation-triangle" size="36"></cds-icon>
        <p>{{ kind() }} not found. It may have been removed, or its host is not responding.</p>
      }
    </div>
  `,
})
export class NotFoundComponent {
  protected readonly inventory = inject(InventoryService);
  readonly kind = input.required<string>();
}
