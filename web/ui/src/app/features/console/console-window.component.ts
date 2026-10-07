import { Component, HostListener, computed, inject, signal, viewChild } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ActionsService, DialogService, InventoryService } from '../../core';
import { ConsoleViewComponent } from './console-view.component';

interface Geometry {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** Which sides a resize handle moves. */
type Edge = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw';

const STORAGE_KEY = 'fc_console_window';
const MIN_W = 360;
const MIN_H = 260;
/** Part of the title bar that must stay on screen, so the window can always be grabbed. */
const GRAB = 80;

function loadGeometry(): Geometry {
  try {
    const g = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null') as Partial<Geometry> | null;
    if (g && [g.x, g.y, g.w, g.h].every((n) => typeof n === 'number' && Number.isFinite(n))) return g as Geometry;
  } catch {
    // unreadable or blocked storage: fall through to the default
  }
  // Default: a 4:3 screen in the lower right, clear of the object navigator.
  const w = Math.min(820, window.innerWidth - 40);
  const h = Math.min(600, window.innerHeight - 100);
  return { x: window.innerWidth - w - 24, y: window.innerHeight - h - 24, w, h };
}

/**
 * The console of a virtual machine in a floating window over the application:
 * it can be moved by its title bar, resized from its edges, maximized, and
 * moved to a browser tab of its own. The rest of the page stays usable.
 */
@Component({
  selector: 'app-console-window',
  imports: [ClarityModule, ConsoleViewComponent],
  template: `
    @if (dialogs.console(); as c) {
      <div
        class="window"
        role="dialog"
        [attr.aria-label]="'Console of ' + title()"
        [class.maximized]="maximized()"
        [class.busy]="busy()"
        [style.left.px]="box().x"
        [style.top.px]="box().y"
        [style.width.px]="box().w"
        [style.height.px]="box().h"
      >
        <div class="titlebar" (pointerdown)="startMove($event)" (dblclick)="toggleMaximize()">
          <cds-icon shape="vm" size="16"></cds-icon>
          <span class="title">{{ title() }}</span>
          <button type="button" class="win-btn" title="Open in a new browser tab" (click)="popOut()"><cds-icon shape="pop-out"></cds-icon></button>
          <button type="button" class="win-btn" [title]="maximized() ? 'Restore' : 'Maximize'" (click)="toggleMaximize()">
            <cds-icon [shape]="maximized() ? 'resize-down' : 'resize-up'"></cds-icon>
          </button>
          <button type="button" class="win-btn" title="Close" (click)="close()"><cds-icon shape="window-close"></cds-icon></button>
        </div>
        <app-console-view #view class="view" [hid]="c.hostId" [vmid]="c.vmId" [compact]="true" />
        @if (!maximized()) {
          @for (e of edges; track e) {
            <div class="handle" [class]="'handle ' + e" (pointerdown)="startResize($event, e)"></div>
          }
        }
      </div>
    }
  `,
  styles: `
    .window {
      position: fixed;
      z-index: 900; /* above the page, below Clarity modals (1050) */
      display: flex;
      flex-direction: column;
      background: #000;
      border: 1px solid #0f171c;
      border-radius: 4px;
      box-shadow: 0 8px 28px rgba(0, 0, 0, 0.45);
      overflow: hidden;
    }
    .window.maximized {
      border-radius: 0;
    }
    /* While moving or resizing, the screen must not swallow the pointer. */
    .window.busy .view {
      pointer-events: none;
    }
    .titlebar {
      display: flex;
      align-items: center;
      gap: 0.35rem;
      flex: 0 0 auto;
      height: 1.5rem;
      padding: 0 0.2rem 0 0.5rem;
      background: #1b2a32;
      color: #fff;
      cursor: move;
      user-select: none;
      touch-action: none;
    }
    .title {
      flex: 1 1 auto;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-size: 0.65rem;
      font-weight: 600;
    }
    .win-btn {
      flex: 0 0 auto;
      display: flex;
      align-items: center;
      justify-content: center;
      width: 1.3rem;
      height: 1.2rem;
      padding: 0;
      border: 0;
      border-radius: 3px;
      background: none;
      color: #d7e0e5;
      cursor: pointer;
    }
    .win-btn:hover {
      background: rgba(255, 255, 255, 0.18);
      color: #fff;
    }
    .view {
      flex: 1 1 auto;
    }
    .handle {
      position: absolute;
      touch-action: none;
    }
    .handle.n,
    .handle.s {
      left: 10px;
      right: 10px;
      height: 6px;
      cursor: ns-resize;
    }
    .handle.e,
    .handle.w {
      top: 10px;
      bottom: 10px;
      width: 6px;
      cursor: ew-resize;
    }
    .handle.n {
      top: 0;
    }
    .handle.s {
      bottom: 0;
    }
    .handle.e {
      right: 0;
    }
    .handle.w {
      left: 0;
    }
    .handle.ne,
    .handle.nw,
    .handle.se,
    .handle.sw {
      width: 12px;
      height: 12px;
    }
    .handle.ne {
      top: 0;
      right: 0;
      cursor: nesw-resize;
    }
    .handle.sw {
      bottom: 0;
      left: 0;
      cursor: nesw-resize;
    }
    .handle.nw {
      top: 0;
      left: 0;
      cursor: nwse-resize;
    }
    .handle.se {
      bottom: 0;
      right: 0;
      cursor: nwse-resize;
    }
  `,
})
export class ConsoleWindowComponent {
  protected readonly dialogs = inject(DialogService);
  private readonly actions = inject(ActionsService);
  private readonly inventory = inject(InventoryService);
  private readonly view = viewChild<ConsoleViewComponent>('view');

  protected readonly edges: Edge[] = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'];
  private readonly geometry = signal(loadGeometry());
  private readonly viewport = signal({ w: window.innerWidth, h: window.innerHeight });
  protected readonly maximized = signal(false);
  /** A move or resize is in progress. */
  protected readonly busy = signal(false);

  protected readonly title = computed(() => {
    const c = this.dialogs.console();
    if (!c) return '';
    return this.inventory.vmsOf(c.hostId).find((v) => v.id === c.vmId)?.name ?? `VM ${c.vmId}`;
  });
  /** The window rectangle, kept inside the browser viewport. */
  protected readonly box = computed<Geometry>(() => {
    const vp = this.viewport();
    if (this.maximized()) return { x: 0, y: 0, w: vp.w, h: vp.h };
    const g = this.geometry();
    const w = Math.max(MIN_W, Math.min(g.w, vp.w));
    const h = Math.max(MIN_H, Math.min(g.h, vp.h));
    return {
      w,
      h,
      x: Math.max(GRAB - w, Math.min(g.x, vp.w - GRAB)),
      y: Math.max(0, Math.min(g.y, vp.h - 30)),
    };
  });

  @HostListener('window:resize')
  protected onViewportResize(): void {
    this.viewport.set({ w: window.innerWidth, h: window.innerHeight });
  }

  protected startMove(ev: PointerEvent): void {
    if (this.maximized() || ev.button !== 0 || (ev.target as HTMLElement).closest('.win-btn')) return;
    const start = this.box();
    this.track(ev, (dx, dy) => ({ ...start, x: start.x + dx, y: start.y + dy }));
  }

  protected startResize(ev: PointerEvent, edge: Edge): void {
    if (ev.button !== 0) return;
    const start = this.box();
    this.track(ev, (dx, dy) => {
      const g = { ...start };
      if (edge.includes('e')) g.w = Math.max(MIN_W, start.w + dx);
      if (edge.includes('s')) g.h = Math.max(MIN_H, start.h + dy);
      if (edge.includes('w')) {
        g.w = Math.max(MIN_W, start.w - dx);
        g.x = start.x + start.w - g.w;
      }
      if (edge.includes('n')) {
        g.h = Math.max(MIN_H, Math.min(start.h - dy, start.y + start.h));
        g.y = start.y + start.h - g.h;
      }
      return g;
    });
  }

  /** Follows the pointer until it is released, applying the geometry `at` computes from its displacement. */
  private track(ev: PointerEvent, at: (dx: number, dy: number) => Geometry): void {
    ev.preventDefault();
    const el = ev.currentTarget as HTMLElement;
    el.setPointerCapture(ev.pointerId);
    this.busy.set(true);
    const move = (e: PointerEvent): void => this.geometry.set(at(e.clientX - ev.clientX, e.clientY - ev.clientY));
    const end = (): void => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', end);
      el.removeEventListener('pointercancel', end);
      this.busy.set(false);
      // Store what is shown: the clamped rectangle, not where the pointer went.
      this.geometry.set(this.box());
      this.save();
      this.view()?.focus();
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', end);
    el.addEventListener('pointercancel', end);
  }

  private save(): void {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.geometry()));
    } catch {
      // storage not available: the window just opens at its default place next time
    }
  }

  protected toggleMaximize(): void {
    this.maximized.update((m) => !m);
    this.view()?.focus();
  }

  protected popOut(): void {
    const c = this.dialogs.console();
    if (c) this.actions.openConsoleTab(c.hostId, c.vmId);
    this.close();
  }

  protected close(): void {
    this.dialogs.console.set(null);
  }
}
