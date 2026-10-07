import { Component, computed, input } from '@angular/core';

import { percent } from '../core';

/** One row of the vSphere "Capacity and Usage" card. */
@Component({
  selector: 'app-usage-meter',
  template: `
    <div class="head">
      <span class="meter-name">{{ label() }}</span>
      <span class="used">{{ usedText() }} used</span>
    </div>
    <div class="track"><div class="fill" [class.warn]="pct() >= 75" [class.danger]="pct() >= 90" [style.width.%]="pct()"></div></div>
    <div class="foot">
      <span>{{ freeText() }} free</span>
      <span>{{ capacityText() }} capacity</span>
    </div>
  `,
  styles: `
    :host {
      display: block;
      margin-bottom: 0.7rem;
      font-size: 0.6rem;
    }
    .head,
    .foot {
      display: flex;
      justify-content: space-between;
    }
    .meter-name {
      font-weight: 600;
      font-size: 0.65rem;
    }
    .used {
      font-size: 0.75rem;
    }
    .foot {
      color: #6c6c6c;
    }
    .track {
      height: 6px;
      margin: 0.2rem 0;
      border-radius: 3px;
      background: #e3e8eb;
      overflow: hidden;
    }
    .fill {
      height: 100%;
      background: #0072a3;
    }
    .fill.warn {
      background: #efc006;
    }
    .fill.danger {
      background: #c21d00;
    }
  `,
})
export class UsageMeterComponent {
  readonly label = input.required<string>();
  readonly used = input.required<number>();
  readonly total = input.required<number>();
  /** Formats a raw value (same unit as used/total) for display. */
  readonly format = input.required<(v: number) => string>();

  readonly pct = computed(() => percent(this.used(), this.total()));
  readonly usedText = computed(() => this.format()(this.used()));
  readonly freeText = computed(() => this.format()(Math.max(0, this.total() - this.used())));
  readonly capacityText = computed(() => this.format()(this.total()));
}
