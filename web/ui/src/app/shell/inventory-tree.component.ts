import { Component, computed, inject, signal } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';
import { ClarityModule } from '@clr/angular';

import { InventoryService } from '../core';
import { ObjectIconComponent } from '../shared/object-icon.component';

type NavView = 'hosts' | 'vms' | 'storage' | 'network';

const VIEWS: { id: NavView; icon: string; title: string }[] = [
  { id: 'hosts', icon: 'cluster', title: 'Hosts and Clusters' },
  { id: 'vms', icon: 'vm', title: 'VMs and Templates' },
  { id: 'storage', icon: 'storage', title: 'Storage' },
  { id: 'network', icon: 'network-globe', title: 'Networking' },
];

/** Left object navigator: four inventory views over the same tree root. */
@Component({
  selector: 'app-inventory-tree',
  imports: [ClarityModule, RouterLink, RouterLinkActive, ObjectIconComponent],
  template: `
    <div class="views" role="tablist">
      @for (v of views; track v.id) {
        <button
          type="button"
          role="tab"
          [class.active]="view() === v.id"
          [title]="v.title"
          [attr.aria-label]="v.title"
          (click)="view.set(v.id)"
        >
          <cds-icon [shape]="v.icon" size="18"></cds-icon>
        </button>
      }
    </div>

    <div class="tree">
      <clr-tree>
        <clr-tree-node [clrExpanded]="true">
          <a class="clr-treenode-link" routerLink="/root" routerLinkActive="active">
            <app-object-icon shape="building" />Hostwarden
          </a>

          <!-- One flat @for per view: nodes nested in @switch would not be projected as tree children. -->
          @for (h of view() === 'hosts' ? inventory.hosts() : []; track h.id) {
            <clr-tree-node [clrExpanded]="true">
              <a
                class="clr-treenode-link"
                [routerLink]="['/host', h.id]"
                routerLinkActive="active"
                [title]="h.address"
              >
                <app-object-icon shape="host" [host]="h" />{{ h.name }}
              </a>
              @for (vm of inventory.vmsOf(h.id); track vm.id) {
                <clr-tree-node>
                  <a
                    class="clr-treenode-link"
                    [routerLink]="['/vm', h.id, vm.id]"
                    routerLinkActive="active"
                  >
                    <app-object-icon shape="vm" [vm]="vm" />{{ vm.name }}
                  </a>
                </clr-tree-node>
              }
            </clr-tree-node>
          }
          @for (vm of view() === 'vms' ? plainVms() : []; track vm.host_id + '/' + vm.id) {
            <clr-tree-node>
              <a
                class="clr-treenode-link"
                [routerLink]="['/vm', vm.host_id, vm.id]"
                routerLinkActive="active"
              >
                <app-object-icon shape="vm" [vm]="vm" />{{ vm.name }}
              </a>
            </clr-tree-node>
          }
          @if (view() === 'vms') {
            <clr-tree-node [clrExpanded]="true">
              <a class="clr-treenode-link"> <app-object-icon shape="folder" />Templates </a>
              @for (vm of templates(); track vm.host_id + '/' + vm.id) {
                <clr-tree-node>
                  <a
                    class="clr-treenode-link"
                    [routerLink]="['/vm', vm.host_id, vm.id]"
                    routerLinkActive="active"
                  >
                    <app-object-icon shape="vm" [vm]="vm" />{{ vm.name }}
                  </a>
                </clr-tree-node>
              }
            </clr-tree-node>
          }
          @for (
            d of view() === 'storage' ? inventory.allDatastores() : [];
            track d.host.id + '/' + d.item.name
          ) {
            <clr-tree-node>
              <a
                class="clr-treenode-link"
                [routerLink]="['/datastore', d.host.id, d.item.name]"
                routerLinkActive="active"
                [title]="d.host.name"
              >
                <app-object-icon shape="storage" />{{ d.item.name }}
              </a>
            </clr-tree-node>
          }
          @for (
            n of view() === 'network' ? inventory.allNetworks() : [];
            track n.host.id + '/' + n.item.name
          ) {
            <clr-tree-node>
              <a
                class="clr-treenode-link"
                [routerLink]="['/network', n.host.id, n.item.name]"
                routerLinkActive="active"
                [title]="n.host.name"
              >
                <app-object-icon shape="network-globe" />{{ n.item.name }}
              </a>
            </clr-tree-node>
          }
        </clr-tree-node>
      </clr-tree>

      @if (!inventory.loaded()) {
        <div class="state"><clr-spinner clrInline></clr-spinner> Loading...</div>
      } @else if (inventory.error()) {
        <div class="state error">{{ inventory.error() }}</div>
      }
    </div>
  `,
  styles: `
    :host {
      display: flex;
      flex-direction: column;
      height: 100%;
      min-height: 0;
      background: #fff;
    }
    .views {
      display: flex;
      flex: 0 0 auto;
      border-bottom: 1px solid var(--fc-border);
    }
    .views button {
      flex: 1;
      height: 1.8rem;
      border: 0;
      border-bottom: 3px solid transparent;
      background: none;
      color: #49677a;
      cursor: pointer;
    }
    .views button:hover {
      background: #f1f6f8;
    }
    .views button.active {
      border-bottom-color: #0072a3;
      color: #0072a3;
    }
    .tree {
      flex: 1 1 auto;
      min-height: 0;
      overflow: auto;
      padding: 0.3rem 0.2rem;
    }
    .clr-treenode-link {
      display: flex;
      align-items: center;
      white-space: nowrap;
      text-decoration: none;
      color: inherit;
    }
    .state {
      padding: 0.4rem 0.6rem;
      font-size: 0.6rem;
      color: #6c6c6c;
    }
    .state.error {
      color: #c21d00;
    }
  `,
})
export class InventoryTreeComponent {
  protected readonly inventory = inject(InventoryService);
  protected readonly views = VIEWS;
  protected readonly view = signal<NavView>('hosts');

  /** "VMs and Templates" view: plain VMs stay at top level, templates live in the Templates folder. */
  protected readonly plainVms = computed(() => this.inventory.allVms().filter((v) => !v.template));
  protected readonly templates = computed(() => this.inventory.allVms().filter((v) => v.template));
}
