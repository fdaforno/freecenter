import { Component, computed, input } from '@angular/core';
import { ClarityModule } from '@clr/angular';

import { Host, Vm } from '../core';

export type IconState = '' | 'on' | 'suspended' | 'error' | 'warn';

/** Icon shape of a VM: templates get their own, as in the vSphere inventory. */
export function vmShape(vm: Vm): string {
  return vm.template ? 'file-settings' : 'vm';
}

export function vmIconState(vm: Vm): IconState {
  if (vm.power_state === 'poweredOn') return 'on';
  return vm.power_state === 'suspended' ? 'suspended' : '';
}

export function hostIconState(host: Host): IconState {
  if (!host.status?.connected) return 'error';
  return host.status.maintenance_mode ? 'warn' : '';
}

/** Inventory object icon with the vSphere-style state overlay. */
@Component({
  selector: 'app-object-icon',
  imports: [ClarityModule],
  template: `<span class="fc-obj-icon" [class]="state()"><cds-icon [shape]="effectiveShape()" [size]="size()"></cds-icon></span>`,
})
export class ObjectIconComponent {
  readonly shape = input.required<string>();
  readonly size = input('16');
  readonly vm = input<Vm>();
  readonly host = input<Host>();
  protected readonly effectiveShape = computed(() => {
    const vm = this.vm();
    return vm ? vmShape(vm) : this.shape();
  });
  readonly state = computed<IconState>(() => {
    const vm = this.vm();
    if (vm) return vmIconState(vm);
    const host = this.host();
    return host ? hostIconState(host) : '';
  });
}
