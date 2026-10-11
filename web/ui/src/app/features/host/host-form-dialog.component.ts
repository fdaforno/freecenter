import { Component, OnInit, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ApiService, DialogService, Host, InventoryService, TaskService, errorMessage } from '../../core';

/** Add Host / Edit Connection Settings. */
@Component({
  selector: 'app-host-form-dialog',
  imports: [ClarityModule, FormsModule],
  template: `
    <clr-modal [clrModalOpen]="true" (clrModalOpenChange)="close()" [clrModalStaticBackdrop]="true" clrModalSize="md">
      <h3 class="modal-title">{{ host() ? 'Edit Connection Settings - ' + host()!.name : 'Add Host' }}</h3>
      <div class="modal-body">
        @if (error()) {
          <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
            <clr-alert-item><span class="alert-text">{{ error() }}</span></clr-alert-item>
          </clr-alert>
        }
        <form clrForm clrLayout="horizontal" (ngSubmit)="save()">
          <clr-input-container>
            <label>Name</label>
            <input clrInput name="name" [(ngModel)]="name" placeholder="esxi-01" />
          </clr-input-container>
          <clr-input-container>
            <label>Host name or IP address</label>
            <input clrInput name="address" [(ngModel)]="address" placeholder="192.168.1.10" />
          </clr-input-container>
          <clr-input-container>
            <label>User name</label>
            <input clrInput name="username" [(ngModel)]="username" autocomplete="off" />
          </clr-input-container>
          <clr-password-container>
            <label>Password</label>
            <input clrPassword name="password" [(ngModel)]="password" autocomplete="new-password" />
            @if (host()) {
              <clr-control-helper>Leave empty to keep the current password</clr-control-helper>
            }
          </clr-password-container>
          <clr-input-container>
            <label>SSH port</label>
            <input clrInput type="number" name="sshPort" [(ngModel)]="sshPort" min="1" max="65535" />
          </clr-input-container>
          @if (!host()) {
            <clr-checkbox-container>
              <label>SSH</label>
              <clr-checkbox-wrapper>
                <input type="checkbox" clrCheckbox name="sshEnabled" [(ngModel)]="sshEnabled" />
                <label>Enabled (required for every change to the host)</label>
              </clr-checkbox-wrapper>
            </clr-checkbox-container>
          }
        </form>
      </div>
      <div class="modal-footer">
        <button type="button" class="btn btn-outline" (click)="close()">Cancel</button>
        <button type="button" class="btn btn-primary" [clrLoading]="busy()" [disabled]="!valid()" (click)="save()">OK</button>
      </div>
    </clr-modal>
  `,
})
export class HostFormDialogComponent implements OnInit {
  private readonly api = inject(ApiService);
  private readonly dialogs = inject(DialogService);
  private readonly inventory = inject(InventoryService);
  private readonly tasks = inject(TaskService);

  readonly host = input<Host>();

  protected name = '';
  protected address = '';
  protected username = 'root';
  protected password = '';
  protected sshPort = 22;
  protected sshEnabled = true;
  protected readonly error = signal('');
  protected readonly busy = signal(false);

  ngOnInit(): void {
    const h = this.host();
    if (!h) return;
    this.name = h.name;
    this.address = h.address;
    this.username = h.username;
    this.sshPort = h.ssh_port || 22;
    this.sshEnabled = h.ssh_enabled;
  }

  protected valid(): boolean {
    return !!this.name.trim() && !!this.address.trim() && !!this.username.trim() && (!!this.host() || !!this.password);
  }

  protected close(): void {
    this.dialogs.hostForm.set(null);
  }

  protected async save(): Promise<void> {
    if (!this.valid() || this.busy()) return;
    const req = {
      name: this.name.trim(),
      address: this.address.trim(),
      username: this.username.trim(),
      password: this.password,
      ssh_port: Number(this.sshPort) || 22,
      ssh_enabled: this.sshEnabled,
    };
    const h = this.host();
    this.error.set('');
    this.busy.set(true);
    try {
      if (h) await this.tasks.run('Reconnect host', req.name, () => this.api.updateHost(h.id, req));
      else await this.tasks.run('Add standalone host', req.name, () => this.api.createHost(req));
      this.close();
    } catch (e) {
      this.error.set(errorMessage(e));
    } finally {
      this.busy.set(false);
      void this.inventory.refresh();
    }
  }
}
