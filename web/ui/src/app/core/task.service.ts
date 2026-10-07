import { Injectable, computed, inject, signal } from '@angular/core';

import { ApiError, ApiService, errorMessage } from './api.service';
import { AuthService } from './auth.service';
import { ServerTask } from './models';

export type TaskStatus = 'running' | 'ok' | 'error';

export interface Task {
  id: number;
  name: string;
  target: string;
  status: TaskStatus;
  details: string;
  initiator: string;
  start: Date;
  end?: Date;
}

const POLL_MS = 3000;
/** Keep polling briefly after an operation, then fall back to the slow rhythm. */
const ACTIVE_FOR_MS = 15000;
const IDLE_EVERY = 5; // poll every 5th tick when nothing is going on

/**
 * The name a task gets on the server. The UI knows what the user asked for
 * ("Power On virtual machine"); it travels as headers on the request that
 * starts the operation.
 */
@Injectable({ providedIn: 'root' })
export class TaskContext {
  private pending: { name: string; target: string } | null = null;

  set(name: string, target: string): void {
    this.pending = { name, target };
  }

  /** Returns the pending name once: only the first modifying request of an operation carries it. */
  take(): { name: string; target: string } | null {
    const p = this.pending;
    this.pending = null;
    return p;
  }

  clear(): void {
    this.pending = null;
  }
}

/**
 * Recent Tasks. The server records every modifying request (of any user or
 * API client) and this service mirrors that list; run() names an operation
 * and makes the list refresh promptly.
 */
@Injectable({ providedIn: 'root' })
export class TaskService {
  private readonly api = inject(ApiService);
  private readonly auth = inject(AuthService);
  private readonly context = inject(TaskContext);

  private readonly server = signal<Task[]>([]);
  /** Failures that never reached the server (e.g. it is unreachable), so it has no record of them. */
  private readonly local = signal<Task[]>([]);
  /** Tasks up to this server id were dismissed with "Clear Recent Tasks". */
  private readonly clearedUpTo = signal(0);
  private timer: ReturnType<typeof setInterval> | null = null;
  private tick = 0;
  private activeUntil = 0;
  private localSeq = 0;

  readonly tasks = computed(() =>
    [...this.local(), ...this.server().filter((t) => t.id > this.clearedUpTo())].sort((a, b) => b.start.getTime() - a.start.getTime()),
  );
  readonly panelOpen = signal(false);

  start(): void {
    if (this.timer) return;
    void this.refresh();
    this.timer = setInterval(() => {
      this.tick++;
      const busy = Date.now() < this.activeUntil || this.server().some((t) => t.status === 'running');
      if (!document.hidden && (busy || this.tick % IDLE_EVERY === 0)) void this.refresh();
    }, POLL_MS);
  }

  stop(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
    this.server.set([]);
    this.local.set([]);
  }

  /** Runs op as a named task. Rethrows on failure after making sure it is visible in Recent Tasks. */
  async run<T>(name: string, target: string, op: () => Promise<T>): Promise<T> {
    this.context.set(name, target);
    this.activeUntil = Date.now() + ACTIVE_FOR_MS;
    try {
      const pending = op();
      // The first request has left: show the running task without waiting for the next tick.
      setTimeout(() => void this.refresh(), 400);
      return await pending;
    } catch (e) {
      if (!(e instanceof ApiError) || e.status === 0) this.addLocalFailure(name, target, errorMessage(e));
      this.panelOpen.set(true);
      throw e;
    } finally {
      this.context.clear();
      this.activeUntil = Date.now() + ACTIVE_FOR_MS;
      void this.refresh();
    }
  }

  clear(): void {
    this.clearedUpTo.set(Math.max(0, ...this.server().map((t) => t.id)));
    this.local.set([]);
  }

  async refresh(): Promise<void> {
    try {
      this.server.set((await this.api.tasks()).map(toTask));
    } catch {
      // Keep the last known list: the inventory already reports connection problems.
    }
  }

  private addLocalFailure(name: string, target: string, details: string): void {
    const now = new Date();
    const task: Task = { id: --this.localSeq, name, target, status: 'error', details, initiator: this.auth.username(), start: now, end: now };
    this.local.update((l) => [task, ...l]);
  }
}

function toTask(t: ServerTask): Task {
  return {
    id: t.id,
    name: t.name,
    target: t.target,
    status: t.status,
    details: t.details,
    initiator: t.initiator,
    start: new Date(t.started_at),
    end: t.finished_at ? new Date(t.finished_at) : undefined,
  };
}
