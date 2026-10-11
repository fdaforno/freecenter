// Regenerates the screenshots and the demo video in docs/media from a
// running Hostwarden instance. See README.md in this folder.
//
//   HW_URL=http://host:8080 HW_USER=admin HW_PASS=... node capture.mjs
//
// Optional: HW_HOST (host name), HW_VM (name of a powered-on VM on that host),
// HW_ONLY=shots|demo to run one part only.

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readdirSync, renameSync, rmSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const here = dirname(fileURLToPath(import.meta.url));
const MEDIA = resolve(here, '../../docs/media');
const VIEWPORT = { width: 1440, height: 780 };

const BASE = (process.env.HW_URL || '').replace(/\/+$/, '');
const USER = process.env.HW_USER || 'admin';
const PASS = process.env.HW_PASS || '';
const ONLY = process.env.HW_ONLY || '';
if (!BASE || !PASS) {
  console.error('Set HW_URL and HW_PASS (and HW_USER if it is not admin).');
  process.exit(1);
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ---------- API: pick the host and the VM to show ----------

async function api(path, token, init = {}) {
  const res = await fetch(BASE + path, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}), ...init.headers },
  });
  if (!res.ok) throw new Error(`${init.method || 'GET'} ${path}: HTTP ${res.status}`);
  return res.json();
}

async function pickTargets() {
  const { token } = await api('/api/v1/auth/login', null, { method: 'POST', body: JSON.stringify({ username: USER, password: PASS }) });
  const hosts = await api('/api/v1/hosts', token);
  const host = process.env.HW_HOST
    ? hosts.find((h) => h.name === process.env.HW_HOST)
    : hosts.find((h) => h.status?.connected) ?? hosts[0];
  if (!host) throw new Error('no host found' + (process.env.HW_HOST ? `: ${process.env.HW_HOST}` : ''));
  const vms = await api(`/api/v1/hosts/${host.id}/vms`, token);
  const vm = process.env.HW_VM
    ? vms.find((v) => v.name === process.env.HW_VM)
    : vms.find((v) => v.power_state === 'poweredOn' && !v.template);
  if (!vm) throw new Error('no powered-on VM found' + (process.env.HW_VM ? `: ${process.env.HW_VM}` : '') + '; start one or set HW_VM');
  console.log(`host: ${host.name}   vm: ${vm.name}`);
  return { host, vm };
}

// ---------- UI helpers ----------

async function login(page) {
  await page.goto(BASE + '/login');
  await page.fill('input[name=username]', USER);
  await page.fill('input[name=password]', PASS);
  await page.click('button[type=submit]');
  await page.waitForURL((u) => !u.pathname.startsWith('/login'));
  await settle(page);
}

async function settle(page, ms = 1500) {
  await page.waitForLoadState('networkidle').catch(() => {});
  await sleep(ms);
}

async function go(page, path, ms) {
  await page.goto(BASE + path);
  await settle(page, ms);
}

async function shot(page, name) {
  await page.mouse.move(0, VIEWPORT.height - 1); // no hover effects in the picture
  await sleep(300);
  await page.screenshot({ path: join(MEDIA, name) });
  console.log('  ' + name);
}

/** Opens the New Virtual Machine wizard and walks it to the cloud-init page. Nothing is created. */
async function wizardToCloudInit(page, host, vmName, typeDelay = 0) {
  await page.getByRole('button', { name: /New Virtual Machine/ }).first().click();
  const wiz = page.locator('clr-wizard');
  await wiz.waitFor();
  const next = () => wiz.getByRole('button', { name: 'Next' }).click();

  await wiz.locator('input[name=creationType][value=cloud]').check({ force: true });
  await next();
  await wiz.locator('input[name=name]').pressSequentially(vmName, { delay: typeDelay });
  const hostRow = wiz.locator('tr', { hasText: host.name });
  if (await hostRow.count()) await hostRow.first().click();
  await next();
  await wiz.locator('tr:has(input[name=datastore])').first().click(); // first datastore
  await next();
  const image = wiz.locator('tr:has(input[name=cloudImage])').first();
  if (!(await image.count())) throw new Error('no cloud image on the first datastore: download one from the wizard catalog first');
  await image.click();
  await next(); // guest OS, pre-filled from the image
  await next();
  await wiz.locator('input[name=ciUser]').pressSequentially('rocky', { delay: typeDelay });
  await wiz.locator('textarea[name=ciKeys]').pressSequentially('ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDemoKeyForScreenshotsOnly rocky@lab', { delay: typeDelay / 4 });
  await wiz.locator('input[name=ciPackages]').pressSequentially('nginx, qemu-guest-agent', { delay: typeDelay });
  await sleep(500);
  return async () => {
    await wiz.getByRole('button', { name: 'Cancel' }).click();
    await sleep(500);
  };
}

async function openConsole(page) {
  await page.locator('button[title="Open console"]').first().click();
  await page.getByText('Connected', { exact: true }).waitFor({ timeout: 20000 }).catch(() => {});
  await sleep(2500);
}

// ---------- screenshots ----------

async function screenshots(browser, { host, vm }) {
  console.log('screenshots:');
  const ctx = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 2 });
  const page = await ctx.newPage();
  await login(page);

  await go(page, `/host/${host.id}/summary`);
  await shot(page, 'host-summary.png');

  await go(page, `/host/${host.id}/monitor`, 4000); // let the charts fill in
  await shot(page, 'host-monitor.png');

  await go(page, `/host/${host.id}/configure`);
  await page.getByRole('button', { name: 'Virtual switches' }).click();
  await settle(page);
  await shot(page, 'host-networks.png');

  await go(page, `/host/${host.id}/summary`);
  const cancel = await wizardToCloudInit(page, host, 'web-01');
  await shot(page, 'new-vm-cloud-init.png');
  await cancel();

  await go(page, `/vm/${host.id}/${vm.id}/summary`);
  await openConsole(page);
  await shot(page, 'vm-console.png');

  await ctx.close();
}

// ---------- demo video ----------

async function demo(browser, { host, vm }) {
  console.log('demo video:');
  const videoDir = join(here, '.video');
  rmSync(videoDir, { recursive: true, force: true });
  const ctx = await browser.newContext({ viewport: VIEWPORT, recordVideo: { dir: videoDir, size: VIEWPORT } });
  const page = await ctx.newPage();

  await page.goto(BASE + '/login');
  await sleep(800);
  await page.locator('input[name=username]').pressSequentially(USER, { delay: 80 });
  await page.fill('input[name=password]', PASS);
  await sleep(400);
  await page.click('button[type=submit]');
  await page.waitForURL((u) => !u.pathname.startsWith('/login'));
  await settle(page, 2500);

  await go(page, `/host/${host.id}/summary`, 2500);
  const cancel = await wizardToCloudInit(page, host, 'web-01', 70);
  await sleep(1500);
  await cancel();

  await go(page, `/vm/${host.id}/${vm.id}/summary`, 2500);
  await openConsole(page);
  await sleep(2500);
  await page.locator('.win-btn[title=Close]').click().catch(() => {});
  await sleep(600);

  await go(page, `/vm/${host.id}/${vm.id}/snapshots`, 2500);
  await go(page, `/host/${host.id}/monitor`, 3500);
  await go(page, `/host/${host.id}/configure`, 800);
  await page.getByRole('button', { name: 'Virtual switches' }).click();
  await settle(page, 2500);

  await ctx.close(); // flushes the video
  const webm = readdirSync(videoDir).find((f) => f.endsWith('.webm'));
  const src = join(videoDir, webm);
  const mp4 = join(MEDIA, 'hostwarden-demo.mp4');
  const gif = join(MEDIA, 'hostwarden-demo.gif');
  try {
    execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-i', src, '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-crf', '28', '-movflags', '+faststart', mp4]);
    execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-i', src, '-vf',
      'fps=8,scale=960:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=128[p];[b][p]paletteuse=dither=bayer:bayer_scale=4', gif]);
    console.log('  hostwarden-demo.mp4\n  hostwarden-demo.gif');
    rmSync(videoDir, { recursive: true, force: true });
  } catch {
    const keep = join(MEDIA, 'hostwarden-demo.webm');
    renameSync(src, keep);
    console.log(`  ffmpeg not found: raw video left in ${keep}; install ffmpeg (brew install ffmpeg) and run again`);
  }
}

// ---------- main ----------

if (!existsSync(MEDIA)) mkdirSync(MEDIA, { recursive: true });
const targets = await pickTargets();
const browser = await chromium.launch();
try {
  if (ONLY !== 'demo') await screenshots(browser, targets);
  if (ONLY !== 'shots') await demo(browser, targets);
} finally {
  await browser.close();
}
console.log('done: check docs/media, then commit');
