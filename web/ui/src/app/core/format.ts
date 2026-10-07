import { Pipe, PipeTransform } from '@angular/core';

import { PowerState } from './models';

export function formatBytes(b: number | null | undefined): string {
  if (b == null || b < 0) return '--';
  if (b < 1024) return `${b} B`;
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  let i = -1;
  let v = b;
  do {
    v /= 1024;
    i++;
  } while (v >= 1024 && i < units.length - 1);
  return `${v.toFixed(v >= 100 ? 0 : 2)} ${units[i]}`;
}

export function formatMb(mb: number | null | undefined): string {
  return mb == null ? '--' : formatBytes(mb * 1024 * 1024);
}

export function formatUptime(s: number | null | undefined): string {
  if (!s || s <= 0) return '--';
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d} days ${h} hours`;
  if (h > 0) return `${h} hours ${m} minutes`;
  return `${m} minutes`;
}

export function percent(used: number, total: number): number {
  return total > 0 ? Math.min(100, Math.max(0, Math.round((used / total) * 100))) : 0;
}

export function powerLabel(s: PowerState): string {
  if (s === 'poweredOn') return 'Powered On';
  if (s === 'suspended') return 'Suspended';
  return 'Powered Off';
}

@Pipe({ name: 'bytes' })
export class BytesPipe implements PipeTransform {
  transform = formatBytes;
}

@Pipe({ name: 'mb' })
export class MbPipe implements PipeTransform {
  transform = formatMb;
}

@Pipe({ name: 'uptime' })
export class UptimePipe implements PipeTransform {
  transform = formatUptime;
}

@Pipe({ name: 'power' })
export class PowerPipe implements PipeTransform {
  transform = powerLabel;
}
