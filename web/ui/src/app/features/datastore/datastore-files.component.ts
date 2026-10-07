import { DatePipe } from '@angular/common';
import { Component, effect, inject, input, signal, untracked } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ActionsService, ApiService, BytesPipe, DialogService, FileInfo, InventoryService, errorMessage } from '../../core';

/** Datastore file browser (the Files tab). */
@Component({
  selector: 'app-datastore-files',
  imports: [ClarityModule, BytesPipe, DatePipe],
  template: `
    <div class="fc-toolbar">
      <button class="btn btn-sm btn-outline" (click)="picker.click()">Upload Files</button>
      <input #picker type="file" hidden (change)="upload(picker)" />
      <button class="btn btn-sm btn-outline" (click)="dialogs.isoDownload.set({ hostId: hostId(), datastore: datastore() })">Download ISO...</button>
      <button
        class="btn btn-sm btn-outline"
        [disabled]="!isVmx(selected())"
        title="Add the virtual machine of the selected .vmx file to the inventory"
        (click)="registerVm()"
      >
        Register VM
      </button>
      <button class="btn btn-sm btn-outline" [disabled]="!selected() || selected()?.is_dir" (click)="download()">Download</button>
      <button class="btn btn-sm btn-outline" [disabled]="!selected()" (click)="remove()">Delete</button>
      <button class="btn btn-sm btn-link" title="Refresh" (click)="load()"><cds-icon shape="refresh"></cds-icon></button>
    </div>

    <div class="crumbs">
      <cds-icon shape="storage" size="14"></cds-icon>
      <button type="button" class="fc-link" (click)="open('/')">{{ datastore() }}</button>
      @for (c of crumbs(); track c.path) {
        <span>/</span><button type="button" class="fc-link" (click)="open(c.path)">{{ c.name }}</button>
      }
    </div>

    @if (error()) {
      <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
        <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
      </clr-alert>
    }

    <table class="table table-compact">
      <thead>
        <tr>
          <th class="left">Name</th>
          <th class="left">Size</th>
          <th class="left">Modified</th>
          <th class="left">Type</th>
          <th class="left">Path</th>
        </tr>
      </thead>
      <tbody>
        @if (loading()) {
          <tr>
            <td colspan="5"><clr-spinner clrInline></clr-spinner> Loading...</td>
          </tr>
        } @else {
          @for (f of files(); track f.name) {
            <tr [class.active]="selected()?.name === f.name" (click)="selected.set(f)" (dblclick)="f.is_dir && open(fullPath(f))">
              <td class="left">
                <cds-icon [shape]="f.is_dir ? 'folder' : 'file'" size="14"></cds-icon>
                @if (f.is_dir) {
                  <button type="button" class="fc-link" (click)="open(fullPath(f)); $event.stopPropagation()">{{ f.name }}</button>
                } @else {
                  {{ f.name }}
                }
              </td>
              <td class="left">{{ f.is_dir ? '' : (f.size | bytes) }}</td>
              <td class="left">{{ f.mod_time | date: 'MM/dd/yyyy, h:mm:ss a' }}</td>
              <td class="left">{{ f.is_dir ? 'Folder' : 'File' }}</td>
              <td class="left">[{{ datastore() }}] {{ fullPath(f).slice(1) }}</td>
            </tr>
          } @empty {
            <tr>
              <td colspan="5" class="fc-muted">This folder is empty</td>
            </tr>
          }
        }
      </tbody>
    </table>
  `,
  styles: `
    .crumbs {
      display: flex;
      align-items: center;
      gap: 0.25rem;
      font-size: 0.65rem;
      margin: 0.3rem 0;
    }
    td cds-icon {
      margin-right: 0.2rem;
    }
    .table {
      margin-top: 0;
    }
    tbody tr {
      cursor: default;
    }
    tr.active td {
      background: #d8e3e9;
    }
  `,
})
export class DatastoreFilesComponent {
  private readonly api = inject(ApiService);
  private readonly inventory = inject(InventoryService);
  private readonly actions = inject(ActionsService);
  protected readonly dialogs = inject(DialogService);

  readonly hostId = input.required<string>();
  readonly datastore = input.required<string>();

  protected readonly path = signal('/');
  protected readonly files = signal<FileInfo[]>([]);
  protected readonly selected = signal<FileInfo | null>(null);
  protected readonly loading = signal(false);
  protected readonly error = signal('');

  constructor() {
    // A different datastore starts again from its root.
    effect(() => {
      this.hostId();
      this.datastore();
      untracked(() => this.open('/'));
    });
  }

  protected crumbs(): { name: string; path: string }[] {
    let acc = '';
    return this.path()
      .split('/')
      .filter(Boolean)
      .map((name) => ({ name, path: (acc += `/${name}`) }));
  }

  /** The API reports each entry's parent folder in `path`, so the full path is built here. */
  protected fullPath(f: FileInfo): string {
    return `${this.path() === '/' ? '' : this.path()}/${f.name}`;
  }

  protected open(path: string): void {
    this.path.set(path);
    void this.load();
  }

  protected async load(): Promise<void> {
    const hid = this.hostId();
    const ds = this.datastore();
    const path = this.path();
    this.loading.set(true);
    this.selected.set(null);
    try {
      const files = await this.api.files(hid, ds, path);
      if (this.hostId() !== hid || this.datastore() !== ds || this.path() !== path) return;
      this.files.set(files.sort((a, b) => Number(b.is_dir) - Number(a.is_dir) || a.name.localeCompare(b.name)));
      this.error.set('');
    } catch (e) {
      this.files.set([]);
      this.error.set(errorMessage(e));
    } finally {
      this.loading.set(false);
    }
  }

  protected async upload(picker: HTMLInputElement): Promise<void> {
    const file = picker.files?.[0];
    picker.value = '';
    if (!file) return;
    const target = `${this.path() === '/' ? '' : this.path()}/${file.name}`;
    await this.actions.track('Upload file to datastore', file.name, () => this.api.uploadFile(this.hostId(), this.datastore(), target, file));
    await this.load();
  }

  protected isVmx(f: FileInfo | null): boolean {
    return !!f && !f.is_dir && f.name.toLowerCase().endsWith('.vmx');
  }

  protected async registerVm(): Promise<void> {
    const f = this.selected();
    if (!f || !this.isVmx(f)) return;
    const target = this.fullPath(f).replace(/^\/+/, '');
    await this.actions.track('Register virtual machine', f.name, () => this.api.registerVm(this.hostId(), this.datastore(), target));
    void this.inventory.refresh();
  }

  /** Saves the selected file through the browser. Meant for logs and configuration files: the content is held in memory. */
  protected async download(): Promise<void> {
    const f = this.selected();
    if (!f || f.is_dir) return;
    try {
      const blob = await this.api.downloadFile(this.hostId(), this.datastore(), this.fullPath(f));
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = f.name;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      this.error.set(errorMessage(e));
    }
  }

  protected async remove(): Promise<void> {
    const f = this.selected();
    if (!f) return;
    const ok = await this.dialogs.confirm({
      title: 'Confirm Delete',
      message: `Delete "${f.name}" from datastore ${this.datastore()}? This action is irreversible.`,
      okLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    const target = this.fullPath(f);
    await this.actions.track('Delete file', f.name, () => this.api.deleteFile(this.hostId(), this.datastore(), target));
    await this.load();
  }
}
