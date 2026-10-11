import { Component, ElementRef, OnDestroy, computed, effect, inject, input, signal, untracked, viewChild } from '@angular/core';
import { ClarityModule } from '@clr/angular';
import RFB from '@novnc/novnc/lib/rfb';

import { ActionsService, AuthService, InventoryService, TaskService } from '../../core';

type ConsoleState = 'connecting' | 'connected' | 'disconnected';

/** The host can accept the session yet never deliver the screen; do not wait forever. */
const CONNECT_TIMEOUT_MS = 15000;

/**
 * Web console of a virtual machine: a toolbar and the screen, filling its
 * container. The WebMKS stream of ESXi is RFB over WebSocket, relayed by the
 * Hostwarden server. Used full-page in its own tab and inside the console
 * window of the shell.
 */
@Component({
  selector: 'app-console-view',
  imports: [ClarityModule],
  template: `
    <header [class.compact]="compact()">
      @if (!compact()) {
        <cds-icon shape="vm" size="20"></cds-icon>
        <span class="name">{{ vmName() }}</span>
      }
      <span class="state" [class]="state()">
        @switch (state()) {
          @case ('connecting') {
            Connecting...
          }
          @case ('connected') {
            Connected
          }
          @case ('disconnected') {
            Disconnected{{ reason() ? ': ' + reason() : '' }}
          }
        }
      </span>
      @if (!compact() && taskError()) {
        <span class="state disconnected">{{ taskError() }}</span>
      }
      <span class="spacer"></span>
      @if (vm(); as v) {
        <button type="button" class="btn btn-sm btn-inverse btn-icon" title="Power On" [disabled]="v.power_state === 'poweredOn'" (click)="actions.vmPower(v, 'on')">
          <cds-icon shape="play" solid></cds-icon>
          @if (!compact()) {
            Power On
          }
        </button>
        <button type="button" class="btn btn-sm btn-inverse btn-icon" title="Shut Down Guest OS" [disabled]="v.power_state !== 'poweredOn'" (click)="actions.vmPower(v, 'shutdown_guest')">
          <cds-icon shape="power"></cds-icon>
          @if (!compact()) {
            Shut Down Guest
          }
        </button>
        <button type="button" class="btn btn-sm btn-inverse btn-icon" title="Power Off" [disabled]="v.power_state !== 'poweredOn'" (click)="actions.vmPower(v, 'off')">
          <cds-icon shape="stop" solid></cds-icon>
          @if (!compact()) {
            Power Off
          }
        </button>
        <span class="sep"></span>
      }
      @if (state() === 'disconnected') {
        <button type="button" class="btn btn-sm btn-inverse" (click)="connect()">Reconnect</button>
      }
      <button type="button" class="btn btn-sm btn-inverse" title="Send Ctrl+Alt+Delete" [disabled]="state() !== 'connected'" (click)="sendCtrlAltDel()">
        {{ compact() ? 'Ctrl+Alt+Del' : 'Send Ctrl+Alt+Delete' }}
      </button>
      <button type="button" class="btn btn-sm btn-inverse" (click)="fullScreen()">Full Screen</button>
    </header>
    <div #screen class="screen"></div>
  `,
  styles: `
    :host {
      display: flex;
      flex-direction: column;
      height: 100%;
      min-height: 0;
      background: #000;
    }
    header {
      display: flex;
      align-items: center;
      gap: 0.5rem;
      flex: 0 0 auto;
      height: 2rem;
      padding: 0 0.6rem;
      background: #1b2a32;
      color: #fff;
      overflow: hidden;
      white-space: nowrap;
    }
    header.compact {
      height: 1.6rem;
      gap: 0.3rem;
      padding: 0 0.4rem;
      background: #25353e;
    }
    .name {
      font-size: 0.7rem;
      font-weight: 600;
    }
    .state {
      font-size: 0.6rem;
      color: #9fb2bd;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .state.connected {
      color: #7fd03f;
    }
    .state.disconnected {
      color: #ff8a80;
    }
    .spacer {
      flex: 1;
    }
    .sep {
      flex: 0 0 1px;
      height: 1.1rem;
      background: rgba(255, 255, 255, 0.3);
    }
    .btn {
      flex: 0 0 auto;
      margin: 0;
    }
    .compact .btn {
      min-width: 0;
      padding: 0 0.3rem;
    }
    .screen {
      flex: 1 1 auto;
      min-height: 0;
    }
  `,
})
export class ConsoleViewComponent implements OnDestroy {
  private readonly auth = inject(AuthService);
  private readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);
  protected readonly actions = inject(ActionsService);
  private readonly screen = viewChild.required<ElementRef<HTMLDivElement>>('screen');
  private rfb: RFB | null = null;
  private wasOn = false;
  private connectTimer: ReturnType<typeof setTimeout> | null = null;

  readonly hid = input.required<string>();
  readonly vmid = input.required<string>();
  /** Inside the console window: a slimmer toolbar, as the window shows the VM name itself. */
  readonly compact = input(false);

  protected readonly state = signal<ConsoleState>('connecting');
  protected readonly reason = signal('');
  protected readonly vm = computed(() => this.inventory.vmsOf(this.hid()).find((v) => v.id === this.vmid()));
  protected readonly vmName = computed(() => this.vm()?.name ?? `VM ${this.vmid()}`);
  /** Where Recent Tasks is not visible (the console tab), a failed power operation is shown in the toolbar. */
  protected readonly taskError = computed(() => {
    // Only this VM's operations: the log is shared by all users and objects.
    const last = this.tasks.tasks().find((t) => t.target === this.vmName());
    return last?.status === 'error' ? `${last.name} failed: ${last.details}` : '';
  });

  constructor() {
    effect(() => {
      this.hid();
      this.vmid();
      this.screen();
      untracked(() => this.connect());
    });
    // The console only exists while the VM runs: attach again once it has been powered on.
    // Only on the off -> on transition, so a VM whose console keeps failing is not retried forever.
    effect(() => {
      const on = this.vm()?.power_state === 'poweredOn';
      untracked(() => {
        if (on && !this.wasOn && this.state() === 'disconnected') this.connect();
        this.wasOn = on;
      });
    });
  }

  ngOnDestroy(): void {
    this.clearTimer();
    this.rfb?.disconnect();
  }

  /** Gives the keyboard to the console. */
  focus(): void {
    this.rfb?.focus();
  }

  protected connect(): void {
    this.rfb?.disconnect();
    this.state.set('connecting');
    this.reason.set('');

    const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
    const path = `/api/v1/hosts/${encodeURIComponent(this.hid())}/vms/${encodeURIComponent(this.vmid())}/console`;
    // Browsers cannot set headers on a WebSocket: the session token goes in the query string.
    const url = `${scheme}://${location.host}${path}?token=${encodeURIComponent(this.auth.token())}`;

    const rfb = new RFB(this.screen().nativeElement, url, { wsProtocols: ['binary'] });
    rfb.scaleViewport = true;
    rfb.background = '#000';
    rfb.addEventListener('connect', () => {
      this.clearTimer();
      this.state.set('connected');
      rfb.focus();
    });
    rfb.addEventListener('disconnect', (e) => {
      if (this.rfb !== rfb) return; // superseded by a reconnect
      this.clearTimer();
      this.state.set('disconnected');
      if (!e.detail.clean && !this.reason()) this.reason.set('connection lost (is the virtual machine powered on?)');
    });
    rfb.addEventListener('securityfailure', (e) => this.reason.set(e.detail.reason || 'security failure'));
    this.rfb = rfb;

    this.clearTimer();
    this.connectTimer = setTimeout(() => {
      this.reason.set('the host did not send the screen of this virtual machine');
      rfb.disconnect();
    }, CONNECT_TIMEOUT_MS);
  }

  private clearTimer(): void {
    if (this.connectTimer) clearTimeout(this.connectTimer);
    this.connectTimer = null;
  }

  protected sendCtrlAltDel(): void {
    this.rfb?.sendCtrlAltDel();
    this.rfb?.focus();
  }

  protected fullScreen(): void {
    void this.screen().nativeElement.requestFullscreen();
  }
}
