import { Component, OnInit, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ApiService, BytesPipe, Datastore, DialogService, InventoryService, TaskService } from '../../core';

interface IsoEntry {
  distro: string;
  version: string;
  variant: string;
  size: string;
  url: string;
}

const ISO_CATALOG: IsoEntry[] = [
  { distro: 'Rocky Linux', version: '9', variant: 'Minimal', size: '~2.0 GB', url: 'https://download.rockylinux.org/pub/rocky/9/isos/x86_64/Rocky-9-latest-x86_64-minimal.iso' },
  { distro: 'Rocky Linux', version: '9', variant: 'DVD', size: '~9.2 GB', url: 'https://download.rockylinux.org/pub/rocky/9/isos/x86_64/Rocky-9-latest-x86_64-dvd.iso' },
  { distro: 'Rocky Linux', version: '9', variant: 'Boot', size: '~0.9 GB', url: 'https://download.rockylinux.org/pub/rocky/9/isos/x86_64/Rocky-9-latest-x86_64-boot.iso' },
  { distro: 'Ubuntu', version: '24.04 LTS', variant: 'Server', size: '~2.9 GB', url: 'https://releases.ubuntu.com/24.04/ubuntu-24.04.4-live-server-amd64.iso' },
  { distro: 'Ubuntu', version: '24.04 LTS', variant: 'Desktop', size: '~5.7 GB', url: 'https://releases.ubuntu.com/24.04/ubuntu-24.04.5.1-desktop-amd64.iso' },
  { distro: 'Ubuntu', version: '22.04 LTS', variant: 'Server', size: '~2.0 GB', url: 'https://releases.ubuntu.com/22.04/ubuntu-22.04.5-live-server-amd64.iso' },
  { distro: 'Fedora', version: '40', variant: 'Server netinst', size: '~0.8 GB', url: 'https://download.fedoraproject.org/pub/fedora/linux/releases/40/Server/x86_64/iso/Fedora-Server-netinst-x86_64-40-1.14.iso' },
  { distro: 'Fedora', version: '40', variant: 'Workstation', size: '~2.2 GB', url: 'https://download.fedoraproject.org/pub/fedora/linux/releases/40/Workstation/x86_64/iso/Fedora-Workstation-netinst-x86_64-40-1.14.iso' },
  { distro: 'Debian', version: '13', variant: 'Netinst', size: '~0.8 GB', url: 'https://cdimage.debian.org/debian-cd/13.7.0/amd64/iso-cd/debian-13.7.0-amd64-netinst.iso' },
  { distro: 'AlmaLinux', version: '9', variant: 'Minimal', size: '~2.0 GB', url: 'https://repo.almalinux.org/almalinux/9/isos/x86_64/AlmaLinux-9-latest-x86_64-minimal.iso' },
];

/** Downloads an ISO image straight to a datastore (runs on the ESXi host). */
@Component({
  selector: 'app-iso-download-dialog',
  imports: [ClarityModule, FormsModule, BytesPipe],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="lg">
      <h3 class="modal-title">Download ISO to Datastore</h3>
      <div class="modal-body">
        <form clrForm clrLayout="horizontal">
          <clr-select-container>
            <label>Datastore</label>
            <select clrSelect name="ds" [(ngModel)]="ds">
              @for (d of stores(); track d.name) {
                <option [value]="d.name">{{ d.name }} ({{ d.free_bytes | bytes }} free)</option>
              }
            </select>
          </clr-select-container>
          <clr-input-container>
            <label>Folder</label>
            <input clrInput name="folder" [(ngModel)]="folder" />
          </clr-input-container>
        </form>

        <table class="table table-compact">
          <thead>
            <tr>
              <th class="left">Distribution</th>
              <th class="left">Version</th>
              <th class="left">Image</th>
              <th class="left">Size</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            @for (iso of catalog; track iso.url) {
              <tr>
                <td class="left">{{ iso.distro }}</td>
                <td class="left">{{ iso.version }}</td>
                <td class="left">{{ iso.variant }}</td>
                <td class="left">{{ iso.size }}</td>
                <td>
                  <button type="button" class="btn btn-link btn-sm" [disabled]="!ds || started().has(iso.url)" (click)="download(iso.url, fileName(iso.url))">
                    {{ started().has(iso.url) ? 'Started' : 'Download' }}
                  </button>
                </td>
              </tr>
            }
          </tbody>
        </table>

        <form clrForm clrLayout="horizontal" (ngSubmit)="downloadCustom()">
          <clr-input-container>
            <label>Custom URL</label>
            <input clrInput name="url" [(ngModel)]="customUrl" placeholder="https://.../custom.iso" size="50" />
          </clr-input-container>
          <clr-input-container>
            <label>File name</label>
            <input clrInput name="name" [(ngModel)]="customName" [placeholder]="fileName(customUrl) || 'file.iso'" />
          </clr-input-container>
        </form>
        <button type="button" class="btn btn-sm" [disabled]="!ds || !customUrl.trim()" (click)="downloadCustom()">Download Custom URL</button>
        <p class="fc-muted">Progress is reported in Recent Tasks. This dialog can be closed while downloads run.</p>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-primary" (click)="close()">Close</button>
      </div>
    </clr-modal>
  `,
})
export class IsoDownloadDialogComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);

  readonly hostId = input.required<string>();
  readonly datastore = input<string>();

  protected readonly catalog = ISO_CATALOG;
  protected readonly stores = signal<Datastore[]>([]);
  protected readonly started = signal(new Set<string>());
  protected ds = '';
  protected folder = 'ISO';
  protected customUrl = '';
  protected customName = '';

  ngOnInit(): void {
    const stores = (this.inventory.datastoresByHost()[this.hostId()] ?? []).filter((d) => d.accessible);
    this.stores.set(stores);
    this.ds = this.datastore() ?? stores[0]?.name ?? '';
  }

  protected close(): void {
    this.dialogs.isoDownload.set(null);
  }

  protected fileName(url: string): string {
    return url.trim().split('/').pop() ?? '';
  }

  protected downloadCustom(): void {
    const url = this.customUrl.trim();
    const name = this.customName.trim() || this.fileName(url);
    if (url && name) this.download(url, name);
  }

  protected download(url: string, name: string): void {
    const hid = this.hostId();
    const ds = this.ds;
    const folder = this.folder.trim() || 'ISO';
    this.started.update((s) => new Set(s).add(url));
    // The server downloads in the background and completes the task itself; a failure to start opens the task panel.
    this.tasks.run(`Download ISO to ${ds}`, name, () => this.api.downloadIso(hid, ds, url, folder, name)).catch(() => undefined);
  }
}
