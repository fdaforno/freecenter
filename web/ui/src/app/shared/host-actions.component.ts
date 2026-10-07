import { Component, inject, input } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { ActionsService, DialogService, Host } from '../core';

/** The ACTIONS dropdown of an ESXi host. */
@Component({
  selector: 'app-host-actions',
  imports: [ClarityModule],
  template: `
    <clr-dropdown>
      <button class="btn btn-link btn-sm fc-actions" clrDropdownTrigger>
        Actions <cds-icon shape="angle" direction="down" size="12"></cds-icon>
      </button>
      <clr-dropdown-menu *clrIfOpen clrPosition="bottom-left">
        <h4 class="dropdown-header">Actions - {{ host().name }}</h4>
        <button clrDropdownItem [clrDisabled]="!connected()" (click)="dialogs.newVm.set({ hostId: host().id })">New Virtual Machine...</button>
        <button clrDropdownItem [clrDisabled]="!connected()" (click)="dialogs.isoDownload.set({ hostId: host().id })">Download ISO to Datastore...</button>
        <div class="dropdown-divider" role="separator"></div>
        <clr-dropdown>
          <button clrDropdownTrigger>Connection</button>
          <clr-dropdown-menu *clrIfOpen clrPosition="right-top">
            <button clrDropdownItem (click)="actions.testHost(host())">Test Connection</button>
            <button clrDropdownItem (click)="dialogs.hostForm.set({ host: host() })">Edit Connection Settings...</button>
          </clr-dropdown-menu>
        </clr-dropdown>
        <clr-dropdown>
          <button clrDropdownTrigger>Maintenance Mode</button>
          <clr-dropdown-menu *clrIfOpen clrPosition="right-top">
            <button clrDropdownItem [clrDisabled]="!connected() || maintenance()" (click)="actions.setMaintenance(host(), true)">Enter Maintenance Mode</button>
            <button clrDropdownItem [clrDisabled]="!connected() || !maintenance()" (click)="actions.setMaintenance(host(), false)">Exit Maintenance Mode</button>
          </clr-dropdown-menu>
        </clr-dropdown>
        <clr-dropdown>
          <button clrDropdownTrigger>Power</button>
          <clr-dropdown-menu *clrIfOpen clrPosition="right-top">
            <button clrDropdownItem [clrDisabled]="!connected()" (click)="actions.hostPower(host(), 'reboot')">Reboot</button>
            <button clrDropdownItem [clrDisabled]="!connected()" (click)="actions.hostPower(host(), 'shutdown')">Shut Down</button>
          </clr-dropdown-menu>
        </clr-dropdown>
        <div class="dropdown-divider" role="separator"></div>
        <button clrDropdownItem (click)="actions.removeHost(host())">Remove from Inventory</button>
      </clr-dropdown-menu>
    </clr-dropdown>
  `,
})
export class HostActionsComponent {
  protected readonly actions = inject(ActionsService);
  protected readonly dialogs = inject(DialogService);

  readonly host = input.required<Host>();

  protected maintenance(): boolean {
    return !!this.host().status?.maintenance_mode;
  }

  protected connected(): boolean {
    return !!this.host().status?.connected;
  }
}
