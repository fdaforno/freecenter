import { Component, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ApiService, errorMessage } from '../../core';

/** Read-only esxcli / vim-cmd console (the server enforces an allowlist). */
@Component({
  selector: 'app-esxcli-console',
  imports: [ClarityModule, FormsModule],
  template: `
    <p class="fc-muted">Read-only esxcli and vim-cmd commands (lists, status, queries). Use the arrow keys for history.</p>
    <form class="line" (ngSubmit)="run()">
      <input class="clr-input fc-mono" name="cmd" placeholder="esxcli storage filesystem list" spellcheck="false" autocomplete="off" [(ngModel)]="command" (keydown)="history($event)" />
      <button type="submit" class="btn btn-sm btn-primary" [clrLoading]="busy()" [disabled]="!command.trim()">Run</button>
    </form>
    <pre class="fc-mono">{{ output() }}</pre>
  `,
  styles: `
    .line {
      display: flex;
      gap: 0.4rem;
      align-items: center;
    }
    .line input {
      flex: 1;
    }
    .line .btn {
      margin: 0;
    }
    pre {
      margin: 0.5rem 0 0;
      padding: 0.5rem;
      min-height: 12rem;
      max-height: 26rem;
      overflow: auto;
      border: 0;
      border-radius: 3px;
      background: #1b2a32;
      color: #d7e0e5;
      white-space: pre;
    }
  `,
})
export class EsxcliConsoleComponent {
  private readonly api = inject(ApiService);

  readonly hostId = input.required<string>();

  protected command = '';
  protected readonly output = signal('Waiting for a command...');
  protected readonly busy = signal(false);
  private readonly past: string[] = [];
  private cursor = 0;

  protected async run(): Promise<void> {
    const cmd = this.command.trim();
    if (!cmd || this.busy()) return;
    this.past.push(cmd);
    this.cursor = this.past.length;
    this.command = '';
    this.output.set(`$ ${cmd}\n...`);
    this.busy.set(true);
    try {
      const res = await this.api.esxcli(this.hostId(), cmd);
      this.output.set(`$ ${cmd}\n${res.output}`);
    } catch (e) {
      this.output.set(`$ ${cmd}\nERROR: ${errorMessage(e)}`);
    } finally {
      this.busy.set(false);
    }
  }

  protected history(e: KeyboardEvent): void {
    if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
    e.preventDefault();
    this.cursor = Math.max(0, Math.min(this.past.length, this.cursor + (e.key === 'ArrowUp' ? -1 : 1)));
    this.command = this.past[this.cursor] ?? '';
  }
}
