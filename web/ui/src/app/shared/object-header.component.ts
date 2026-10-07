import { Component, input } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';

import { Host, Vm } from '../core';
import { ObjectIconComponent } from './object-icon.component';

export interface ObjectTab {
  id: string;
  label: string;
}

/** vSphere object header: icon, name, quick actions, ACTIONS menu and the tab bar. */
@Component({
  selector: 'app-object-header',
  imports: [RouterLink, RouterLinkActive, ObjectIconComponent],
  template: `
    <div class="title-row">
      <app-object-icon [shape]="icon()" size="24" [vm]="vm()" [host]="host()" />
      <span class="name" [title]="title()">{{ title() }}</span>
      <span class="divider"></span>
      <ng-content />
    </div>
    <ul class="nav" role="tablist">
      @for (tab of tabs(); track tab.id) {
        <li role="presentation" class="nav-item">
          <a class="btn btn-link nav-link" role="tab" [routerLink]="base().concat(tab.id)" routerLinkActive="active">{{ tab.label }}</a>
        </li>
      }
    </ul>
  `,
  styles: `
    :host {
      display: block;
      flex: 0 0 auto;
      background: #fff;
      padding: 0.5rem 0.9rem 0;
    }
    .title-row {
      display: flex;
      align-items: center;
      gap: 0.4rem;
      min-height: 1.6rem;
    }
    .name {
      font-size: 1rem;
      font-weight: 400;
      color: #000;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .divider {
      width: 1px;
      height: 1.2rem;
      background: #cdd4d9;
      margin: 0 0.3rem;
    }
    .nav {
      margin-top: 0.3rem;
      height: 1.6rem;
    }
    .nav .nav-link {
      font-size: 0.65rem;
      text-transform: none;
      letter-spacing: normal;
    }
  `,
})
export class ObjectHeaderComponent {
  readonly icon = input.required<string>();
  readonly title = input.required<string>();
  readonly base = input.required<string[]>();
  readonly tabs = input.required<ObjectTab[]>();
  readonly vm = input<Vm>();
  readonly host = input<Host>();
}
