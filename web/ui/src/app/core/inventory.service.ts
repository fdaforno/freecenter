import { Injectable, computed, inject, signal } from '@angular/core';

import { ApiService, errorMessage } from './api.service';
import { Datastore, Host, HostScoped, Network, Vm } from './models';

const POLL_MS = 20000;

/** Holds the whole inventory (hosts and their VMs, datastores, networks) and keeps it fresh. */
@Injectable({ providedIn: 'root' })
export class InventoryService {
  private readonly api = inject(ApiService);
  private timer: ReturnType<typeof setInterval> | null = null;
  private inFlight: Promise<void> | null = null;

  readonly hosts = signal<Host[]>([]);
  readonly vmsByHost = signal<Record<string, Vm[]>>({});
  readonly datastoresByHost = signal<Record<string, Datastore[]>>({});
  readonly networksByHost = signal<Record<string, Network[]>>({});
  readonly loaded = signal(false);
  readonly loading = signal(false);
  readonly error = signal('');
  /** Bumped after every refresh so detail pages know to reload their own data. */
  readonly revision = signal(0);

  readonly allVms = computed(() => this.hosts().flatMap((h) => this.vmsByHost()[h.id] ?? []));
  readonly allDatastores = computed<HostScoped<Datastore>[]>(() =>
    this.hosts().flatMap((host) => (this.datastoresByHost()[host.id] ?? []).map((item) => ({ host, item }))),
  );
  readonly allNetworks = computed<HostScoped<Network>[]>(() =>
    this.hosts().flatMap((host) => (this.networksByHost()[host.id] ?? []).map((item) => ({ host, item }))),
  );

  start(): void {
    if (this.timer) return;
    void this.refresh();
    this.timer = setInterval(() => {
      if (!document.hidden) void this.refresh();
    }, POLL_MS);
  }

  stop(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
    this.hosts.set([]);
    this.vmsByHost.set({});
    this.datastoresByHost.set({});
    this.networksByHost.set({});
    this.loaded.set(false);
  }

  /** Reloads everything; concurrent callers share one request round. */
  refresh(): Promise<void> {
    this.inFlight ??= this.load().finally(() => (this.inFlight = null));
    return this.inFlight;
  }

  /** VMs of one host; empty while the host is unknown or disconnected. */
  vmsOf(hid: string): Vm[] {
    return (this.vmsByHost() as Partial<Record<string, Vm[]>>)[hid] ?? [];
  }

  /** Datastores of one host; empty while the host is unknown or disconnected. */
  datastoresOf(hid: string): Datastore[] {
    return (this.datastoresByHost() as Partial<Record<string, Datastore[]>>)[hid] ?? [];
  }

  host(hid: string): Host | undefined {
    return this.hosts().find((h) => h.id === hid);
  }

  hostName(hid: string): string {
    return this.host(hid)?.name ?? hid;
  }

  private async load(): Promise<void> {
    this.loading.set(true);
    try {
      const hosts = await this.api.hosts();
      // Show the hosts right away; their VMs, datastores and networks fill in below.
      this.hosts.set(hosts);
      this.loaded.set(true);
      const vms: Record<string, Vm[]> = {};
      const dss: Record<string, Datastore[]> = {};
      const nets: Record<string, Network[]> = {};
      await Promise.all(
        hosts
          .filter((h) => h.status?.connected)
          .map(async (h) => {
            const [v, d, n] = await Promise.all([
              this.api.vms(h.id).catch(() => this.vmsByHost()[h.id] ?? []),
              this.api.datastores(h.id).catch(() => this.datastoresByHost()[h.id] ?? []),
              this.api.networks(h.id).catch(() => this.networksByHost()[h.id] ?? []),
            ]);
            vms[h.id] = v.map((x) => ({ ...x, host_id: h.id })).sort((a, b) => a.name.localeCompare(b.name));
            dss[h.id] = d;
            nets[h.id] = n;
          }),
      );
      this.vmsByHost.set(vms);
      this.datastoresByHost.set(dss);
      this.networksByHost.set(nets);
      this.error.set('');
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.loaded.set(true);
      this.loading.set(false);
      this.revision.update((r) => r + 1);
    }
  }
}
