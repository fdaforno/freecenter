import { DatePipe, DecimalPipe } from '@angular/common';
import { Component, ElementRef, OnDestroy, computed, effect, input, signal, viewChild } from '@angular/core';

/** Space around the plot for axis labels and the end-of-line value, in pixels. */
const PAD = { top: 14, right: 58, bottom: 24, left: 44 };
/** Size used until the container has been measured, and the smallest plot drawn. */
const DEFAULT_SIZE = { width: 560, height: 190 };
const MIN_HEIGHT = 120;

interface Tick {
  y: number;
  label: string;
}

/**
 * A single-series time chart, as in the vSphere performance overview: one
 * measure per chart (never two scales on one plot), a thin line over a light
 * wash, the current value labelled at the end of the line, and a crosshair
 * with a tooltip on hover. The data is also available as a table. The plot is
 * drawn at the pixel size of its container, so it fills whatever card it is in
 * without stretching the text.
 */
@Component({
  selector: 'app-line-chart',
  imports: [DatePipe, DecimalPipe],
  template: `
    <div class="head">
      <span class="title">{{ title() }}</span>
      <span class="unit">{{ unit() }}</span>
    </div>
    @if (values().length < 2) {
      <div class="empty">No data collected yet</div>
    } @else {
      <div class="plot" #plot>
        <svg [attr.viewBox]="viewBox()" role="img" [attr.aria-label]="title() + ', last hour'" (mousemove)="hover($event)" (mouseleave)="hoverIndex.set(null)">
          @for (t of yTicks(); track t.y) {
            <line class="grid" [attr.x1]="left" [attr.x2]="right()" [attr.y1]="t.y" [attr.y2]="t.y" />
            <text class="axis" [attr.x]="left - 6" [attr.y]="t.y + 3" text-anchor="end">{{ t.label }}</text>
          }
          @for (t of xTicks(); track t.x) {
            <text class="axis" [attr.x]="t.x" [attr.y]="bottom() + 15" [attr.text-anchor]="t.anchor">{{ t.time | date: 'HH:mm' }}</text>
          }
          <path class="area" [attr.d]="areaPath()" />
          <path class="line" [attr.d]="linePath()" />
          <circle class="end" [attr.cx]="right()" [attr.cy]="yOf(last())" r="4" />
          <text class="value" [attr.x]="right() + 8" [attr.y]="yOf(last()) + 4">{{ last() | number: format() }}</text>
          @if (hovered(); as h) {
            <line class="cross" [attr.x1]="h.x" [attr.x2]="h.x" [attr.y1]="top" [attr.y2]="bottom()" />
            <circle class="end" [attr.cx]="h.x" [attr.cy]="h.y" r="4" />
          }
        </svg>
        @if (hovered(); as h) {
          <div class="tip" [style.left.%]="h.leftPct" [class.flip]="h.leftPct > 60">
            <strong>{{ h.value | number: format() }} {{ unit() }}</strong>
            <span>{{ h.time | date: 'MMM d, HH:mm:ss' }}</span>
          </div>
        }
      </div>
      <details>
        <summary>Data table</summary>
        <div class="table-scroll">
          <table class="table table-compact">
            <thead>
              <tr>
                <th class="left">Time</th>
                <th class="left">{{ title() }} ({{ unit() }})</th>
              </tr>
            </thead>
            <tbody>
              @for (v of values(); track $index) {
                <tr>
                  <td class="left">{{ times()[$index] | date: 'HH:mm:ss' }}</td>
                  <td class="left">{{ v | number: format() }}</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      </details>
    }
  `,
  styles: `
    :host {
      display: flex;
      flex-direction: column;
      height: 100%;
      min-height: 0;
    }
    .head {
      flex: 0 0 auto;
      display: flex;
      align-items: baseline;
      gap: 0.3rem;
    }
    .title {
      font-size: 0.65rem;
      font-weight: 600;
      color: #21333b;
    }
    .unit {
      font-size: 0.55rem;
      color: #6c6c6c;
    }
    .plot {
      position: relative;
      flex: 1 1 auto;
      min-height: 120px;
    }
    svg {
      position: absolute;
      inset: 0;
      width: 100%;
      height: 100%;
    }
    .grid {
      stroke: #e3e8eb;
      stroke-width: 1;
    }
    .axis {
      font-size: 10px;
      fill: #6c6c6c;
    }
    .line {
      fill: none;
      stroke: #0072a3;
      stroke-width: 2;
      stroke-linejoin: round;
      stroke-linecap: round;
    }
    .area {
      fill: #0072a3;
      opacity: 0.1;
    }
    .end {
      fill: #0072a3;
      stroke: #fff;
      stroke-width: 2;
    }
    .value {
      font-size: 11px;
      font-weight: 600;
      fill: #21333b;
    }
    .cross {
      stroke: #8c9aa3;
      stroke-width: 1;
    }
    .tip {
      position: absolute;
      top: 0;
      transform: translateX(8px);
      display: flex;
      flex-direction: column;
      padding: 0.15rem 0.35rem;
      border: 1px solid #cdd4d9;
      border-radius: 3px;
      background: #fff;
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
      font-size: 0.55rem;
      line-height: 0.75rem;
      color: #21333b;
      white-space: nowrap;
      pointer-events: none;
    }
    .tip.flip {
      transform: translateX(calc(-100% - 8px));
    }
    .tip span {
      color: #6c6c6c;
    }
    .empty {
      padding: 2rem 0;
      text-align: center;
      font-size: 0.6rem;
      color: #6c6c6c;
    }
    details {
      flex: 0 0 auto;
      font-size: 0.55rem;
      color: #6c6c6c;
    }
    summary {
      cursor: pointer;
    }
    .table-scroll {
      max-height: 9rem;
      overflow: auto;
    }
    .table {
      margin: 0.2rem 0 0;
    }
  `,
})
export class LineChartComponent implements OnDestroy {
  private readonly plot = viewChild<ElementRef<HTMLDivElement>>('plot');
  private readonly size = signal(DEFAULT_SIZE);
  private readonly observer = new ResizeObserver((entries) => {
    const box = entries[0].contentRect;
    if (box.width > 0) this.size.set({ width: Math.round(box.width), height: Math.max(MIN_HEIGHT, Math.round(box.height)) });
  });
  private observed: Element | null = null;

  constructor() {
    // The plot element only exists while there is data: follow whichever one is rendered.
    effect(() => {
      const el = this.plot()?.nativeElement ?? null;
      if (el === this.observed) return;
      if (this.observed) this.observer.unobserve(this.observed);
      this.observed = el;
      if (el) this.observer.observe(el);
    });
  }

  readonly title = input.required<string>();
  readonly unit = input.required<string>();
  readonly times = input.required<string[]>();
  readonly values = input.required<number[]>();
  /** Top of the scale; unset = fitted to the data. */
  readonly max = input<number>();

  protected readonly viewBox = computed(() => `0 0 ${this.size().width} ${this.size().height}`);
  protected readonly left = PAD.left;
  protected readonly right = computed(() => this.size().width - PAD.right);
  protected readonly top = PAD.top;
  protected readonly bottom = computed(() => this.size().height - PAD.bottom);
  protected readonly hoverIndex = signal<number | null>(null);

  /** A round number at or above the largest value, so the ticks are clean. */
  private readonly scaleMax = computed(() => {
    const fixed = this.max();
    if (fixed) return fixed;
    const peak = Math.max(1, ...this.values());
    const magnitude = 10 ** Math.floor(Math.log10(peak));
    return [1, 2, 5, 10].map((m) => m * magnitude).find((m) => m >= peak) ?? peak;
  });
  /** Decimals only where they carry information. */
  protected readonly format = computed(() => (this.scaleMax() <= 10 ? '1.1-1' : '1.0-0'));
  protected readonly last = computed(() => this.values()[this.values().length - 1] ?? 0);

  protected readonly yTicks = computed<Tick[]>(() =>
    [0, 0.5, 1].map((f) => {
      const v = this.scaleMax() * f;
      return { y: this.yOf(v), label: v >= 1000 ? `${(v / 1000).toLocaleString('en-US')}k` : v.toLocaleString('en-US') };
    }),
  );
  protected readonly xTicks = computed(() => {
    const t = this.times();
    const n = t.length;
    return [
      { x: this.left, time: t[0], anchor: 'start' },
      { x: (this.left + this.right()) / 2, time: t[Math.floor((n - 1) / 2)], anchor: 'middle' },
      { x: this.right(), time: t[n - 1], anchor: 'end' },
    ];
  });
  protected readonly linePath = computed(() =>
    this.values()
      .map((v, i) => `${i === 0 ? 'M' : 'L'}${this.xOf(i).toFixed(1)},${this.yOf(v).toFixed(1)}`)
      .join(' '),
  );
  protected readonly areaPath = computed(() => `${this.linePath()} L${this.right()},${this.bottom()} L${this.left},${this.bottom()} Z`);
  protected readonly hovered = computed(() => {
    const i = this.hoverIndex();
    if (i === null || i >= this.values().length) return null;
    const x = this.xOf(i);
    return { x, y: this.yOf(this.values()[i]), value: this.values()[i], time: this.times()[i], leftPct: (x / this.size().width) * 100 };
  });

  ngOnDestroy(): void {
    this.observer.disconnect();
  }

  protected xOf(i: number): number {
    const n = Math.max(1, this.values().length - 1);
    return this.left + ((this.right() - this.left) * i) / n;
  }

  protected yOf(v: number): number {
    const f = Math.min(1, Math.max(0, v / this.scaleMax()));
    return this.bottom() - (this.bottom() - this.top) * f;
  }

  /** Snaps the crosshair to the sample nearest to the pointer. */
  protected hover(e: MouseEvent): void {
    const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * this.size().width;
    const n = this.values().length - 1;
    const i = Math.round(((x - this.left) / (this.right() - this.left)) * n);
    this.hoverIndex.set(Math.min(n, Math.max(0, i)));
  }
}
