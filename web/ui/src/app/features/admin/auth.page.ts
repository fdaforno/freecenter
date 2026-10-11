import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ClarityModule } from '@clr/angular';

import { ApiService, OIDCConfig, Role, errorMessage } from '../../core';

@Component({
  selector: 'app-auth-page',
  imports: [ClarityModule, FormsModule],
  template: `
    <div class="fc-page">
      <div class="fc-page-body">
        <h2>Authentication</h2>

        <!-- OIDC Configuration -->
        <div class="card">
          <div class="card-header">OIDC Provider</div>
          <div class="card-block">
            @if (oidcError()) {
              <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
                <clr-alert-item><span class="alert-text">{{ oidcError() }}</span></clr-alert-item>
              </clr-alert>
            }
            @if (oidcLoaded()) {
              <form (ngSubmit)="saveOIDC()">
                <clr-toggle-container>
                  <clr-toggle-wrapper>
                    <input type="checkbox" clrToggle name="enabled" [(ngModel)]="cfg.enabled" />
                    <label>Enable OIDC authentication</label>
                  </clr-toggle-wrapper>
                </clr-toggle-container>

                <clr-input-container>
                  <label>Issuer URL</label>
                  <input type="text" clrInput name="issuer" [(ngModel)]="cfg.issuer"
                    placeholder="http://192.168.251.20:30081/realms/hostwarden" />
                </clr-input-container>

                <clr-input-container>
                  <label>Client ID</label>
                  <input type="text" clrInput name="client_id" [(ngModel)]="cfg.client_id" />
                </clr-input-container>

                <clr-input-container>
                  <label>Client Secret</label>
                  <input type="password" clrInput name="client_secret" [(ngModel)]="cfg.client_secret"
                    [placeholder]="hasSecret ? '(unchanged)' : 'enter the client secret'" />
                </clr-input-container>

                <clr-input-container>
                  <label>Scopes (space-separated)</label>
                  <input type="text" clrInput name="scopes" [ngModel]="scopesStr()" (ngModelChange)="cfg.scopes = $event.split(' ').filter(s => s)" />
                </clr-input-container>

                <clr-input-container>
                  <label>Groups claim name</label>
                  <input type="text" clrInput name="claim_name" [(ngModel)]="cfg.claim_name"
                    placeholder="groups" />
                </clr-input-container>

                <h6>Group → Role Mapping</h6>
                <p class="fc-muted">Map Keycloak groups to Hostwarden roles. Users in a group get the permissions of the mapped role.</p>
                @for (item of groupMappings(); track $index) {
                  <div class="mapping-row">
                    <input type="text" class="clr-input" [ngModel]="item.group" (ngModelChange)="updateGroup($index, $event, item.role)"
                      placeholder="Keycloak group name" />
                    <select class="clr-select" [ngModel]="item.role" (ngModelChange)="updateGroup($index, item.group, $event)">
                      @for (r of roles(); track r.id) {
                        <option [value]="r.id">{{ r.name }}</option>
                      }
                    </select>
                    <button type="button" class="btn btn-sm btn-icon" (click)="removeGroup($index)">
                      <cds-icon shape="times" size="16"></cds-icon>
                    </button>
                  </div>
                }
                <button type="button" class="btn btn-sm btn-link" (click)="addGroup()">+ Add mapping</button>

                <clr-select-container>
                  <label>Default role for OIDC users</label>
                  <select clrSelect name="default_role" [(ngModel)]="cfg.default_role">
                    <option value="">(none — deny unmapped)</option>
                    @for (r of roles(); track r.id) {
                      <option [value]="r.id">{{ r.name }}</option>
                    }
                  </select>
                </clr-select-container>

                <div class="fc-toolbar">
                  <button type="submit" class="btn btn-primary" [disabled]="oidcSaving()">{{ oidcSaving() ? 'Saving...' : 'Save OIDC Configuration' }}</button>
                </div>
              </form>
            } @else {
              <clr-spinner clrMedium></clr-spinner>
            }
          </div>
        </div>

        <!-- Role Definitions -->
        <div class="card">
          <div class="card-header">Roles &amp; Permissions</div>
          <div class="card-block">
            @if (roleError()) {
              <clr-alert clrAlertType="danger" [clrAlertClosable]="false">
                <clr-alert-item><span class="alert-text">{{ roleError() }}</span></clr-alert-item>
              </clr-alert>
            }
            <table class="table">
              <thead>
                <tr>
                  <th class="left">Role</th>
                  <th class="left">Description</th>
                  <th class="center">VMs</th>
                  <th class="center">Hosts</th>
                  <th class="center">Storage</th>
                  <th class="center">Users</th>
                  <th class="center">Console</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                @for (r of roles(); track r.id) {
                  <tr>
                    <td class="left">
                      {{ r.name }}
                      @if (r.builtin) { <span class="label label-info">built-in</span> }
                    </td>
                    <td class="left">{{ r.description || '--' }}</td>
                    <td class="center"><cds-icon [shape]="r.manage_vms ? 'check' : 'times'" [status]="r.manage_vms ? 'success' : ''" size="16"></cds-icon></td>
                    <td class="center"><cds-icon [shape]="r.manage_hosts ? 'check' : 'times'" [status]="r.manage_hosts ? 'success' : ''" size="16"></cds-icon></td>
                    <td class="center"><cds-icon [shape]="r.manage_storage ? 'check' : 'times'" [status]="r.manage_storage ? 'success' : ''" size="16"></cds-icon></td>
                    <td class="center"><cds-icon [shape]="r.manage_users ? 'check' : 'times'" [status]="r.manage_users ? 'success' : ''" size="16"></cds-icon></td>
                    <td class="center"><cds-icon [shape]="r.console_access ? 'check' : 'times'" [status]="r.console_access ? 'success' : ''" size="16"></cds-icon></td>
                    <td>
                      <button type="button" class="btn btn-sm btn-link" (click)="editRole(r)">Edit</button>
                      @if (!r.builtin) {
                        <button type="button" class="btn btn-sm btn-link" (click)="deleteRole(r)">Delete</button>
                      }
                    </td>
                  </tr>
                }
              </tbody>
            </table>
            <button type="button" class="btn btn-sm btn-link" (click)="newRole()">+ New role</button>
          </div>
        </div>

        <!-- Role editor modal -->
        @if (editing()) {
          <clr-modal [clrModalOpen]="true" [clrModalClosable]="false">
            <h3 class="modal-title">{{ editing()!.id ? 'Edit Role' : 'New Role' }}</h3>
            <div class="modal-body">
              <clr-input-container>
                <label>Name</label>
                <input type="text" clrInput [(ngModel)]="editing()!.name" />
              </clr-input-container>
              <clr-textarea-container>
                <label>Description</label>
                <textarea clrTextarea [(ngModel)]="editing()!.description"></textarea>
              </clr-textarea-container>
              <h6>Permissions</h6>
              <clr-checkbox-container>
                <clr-checkbox-wrapper>
                  <input type="checkbox" clrCheckbox [(ngModel)]="editing()!.manage_vms" />
                  <label>Manage virtual machines (create, edit, power, clone, migrate, snapshots, backups)</label>
                </clr-checkbox-wrapper>
                <clr-checkbox-wrapper>
                  <input type="checkbox" clrCheckbox [(ngModel)]="editing()!.manage_hosts" />
                  <label>Manage hosts (configuration, services, network, power, maintenance)</label>
                </clr-checkbox-wrapper>
                <clr-checkbox-wrapper>
                  <input type="checkbox" clrCheckbox [(ngModel)]="editing()!.manage_storage" />
                  <label>Manage storage (upload, delete, ISO and cloud image downloads)</label>
                </clr-checkbox-wrapper>
                <clr-checkbox-wrapper>
                  <input type="checkbox" clrCheckbox [(ngModel)]="editing()!.manage_users" />
                  <label>Manage users, roles and OIDC configuration</label>
                </clr-checkbox-wrapper>
                <clr-checkbox-wrapper>
                  <input type="checkbox" clrCheckbox [(ngModel)]="editing()!.console_access" />
                  <label>VM console access (keyboard and mouse)</label>
                </clr-checkbox-wrapper>
              </clr-checkbox-container>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-outline" (click)="editing.set(null)">Cancel</button>
              <button type="button" class="btn btn-primary" (click)="saveRole()">Save</button>
            </div>
          </clr-modal>
        }
      </div>
    </div>
  `,
  styles: [`
    .fc-page { display: flex; flex-direction: column; height: 100%; }
    .fc-page-body { flex: 1 1 auto; overflow: auto; padding: 0.6rem 0.9rem 0.9rem; display: flex; flex-direction: column; gap: 0.6rem; }
    h2 { margin: 0 0 0.3rem; font-size: 1rem; font-weight: 400; }
    h6 { margin: 0.8rem 0 0.3rem; }
    .card { margin: 0; }
    .table { font-size: 0.65rem; }
    .center { text-align: center; }
    .mapping-row { display: flex; gap: 0.4rem; align-items: center; margin-bottom: 0.3rem; }
    .mapping-row input { flex: 1; }
    .mapping-row select { width: 10rem; }
    .fc-toolbar { margin-top: 0.6rem; }
    .fc-muted { color: #6c6c6c; font-size: 0.65rem; }
  `],
})
export class AuthPage implements OnInit {
  private readonly api = inject(ApiService);

  protected readonly roles = signal<Role[]>([]);
  protected readonly oidcLoaded = signal(false);
  protected readonly oidcSaving = signal(false);
  protected readonly oidcError = signal('');
  protected readonly roleError = signal('');
  protected readonly editing = signal<Role | null>(null);
  protected hasSecret = false;

  protected cfg: OIDCConfig = { enabled: false, issuer: '', client_id: '', client_secret: '', scopes: ['openid', 'profile', 'email'], claim_name: 'groups', group_roles: {}, default_role: '' };
  private groupList: { group: string; role: string }[] = [];

  protected groupMappings = signal<{ group: string; role: string }[]>([]);
  protected scopesStr = signal('openid profile email');

  ngOnInit(): void {
    void this.loadRoles();
    void this.loadOIDC();
  }

  private async loadRoles(): Promise<void> {
    try {
      const r = await this.api.roles();
      this.roles.set(r);
    } catch (e) {
      this.roleError.set(errorMessage(e));
    }
  }

  private async loadOIDC(): Promise<void> {
    try {
      const cfg = await this.api.oidcConfig();
      this.cfg = { ...cfg, client_secret: '' };
      this.hasSecret = !!cfg.client_secret || cfg.client_secret === '';
      this.scopesStr.set((cfg.scopes ?? ['openid', 'profile', 'email']).join(' '));
      this.groupList = Object.entries(cfg.group_roles ?? {}).map(([group, role]) => ({ group, role }));
      this.groupMappings.set(this.groupList);
      this.oidcLoaded.set(true);
    } catch (e) {
      this.oidcError.set(errorMessage(e));
      this.oidcLoaded.set(true);
    }
  }

  protected addGroup(): void {
    this.groupList = [...this.groupList, { group: '', role: this.roles()[0]?.id ?? '' }];
    this.groupMappings.set(this.groupList);
  }

  protected removeGroup(i: number): void {
    this.groupList = this.groupList.filter((_, idx) => idx !== i);
    this.groupMappings.set(this.groupList);
  }

  protected updateGroup(i: number, group: string, role: string): void {
    this.groupList[i] = { group, role };
    this.groupMappings.set([...this.groupList]);
  }

  protected async saveOIDC(): Promise<void> {
    this.oidcError.set('');
    this.oidcSaving.set(true);
    try {
      this.cfg.group_roles = {};
      for (const m of this.groupList) {
        if (m.group) this.cfg.group_roles[m.group] = m.role;
      }
      const saved = await this.api.updateOIDCConfig(this.cfg);
      this.cfg = { ...saved, client_secret: '' };
      this.hasSecret = true;
    } catch (e) {
      this.oidcError.set(errorMessage(e));
    } finally {
      this.oidcSaving.set(false);
    }
  }

  protected newRole(): void {
    this.editing.set({ id: '', name: '', description: '', manage_vms: false, manage_hosts: false, manage_storage: false, manage_users: false, console_access: false });
  }

  protected editRole(r: Role): void {
    this.editing.set({ ...r });
  }

  protected async saveRole(): Promise<void> {
    const r = this.editing();
    if (!r || !r.name) return;
    try {
      if (r.id) {
        await this.api.updateRole(r.id, r);
      } else {
        await this.api.createRole(r);
      }
      this.editing.set(null);
      await this.loadRoles();
    } catch (e) {
      this.roleError.set(errorMessage(e));
    }
  }

  protected async deleteRole(r: Role): Promise<void> {
    if (!confirm(`Delete role "${r.name}"? Users assigned to it will lose access.`)) return;
    try {
      await this.api.deleteRole(r.id);
      await this.loadRoles();
    } catch (e) {
      this.roleError.set(errorMessage(e));
    }
  }
}
