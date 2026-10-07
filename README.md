# FreeCenter

**A vCenter-style management console for the free VMware ESXi hypervisor.**

FreeCenter gives standalone ESXi hosts the things you normally need vCenter for: one web console for all your hosts, virtual machine lifecycle, templates and clones, migration between hosts, backups, scheduled tasks, monitoring, users and roles, a REST API and a Terraform provider. It runs as a single small container next to your hosts and needs nothing installed on them.

![FreeCenter demo: add a host, deploy a VM from a cloud image, open its console, take a snapshot](docs/media/freecenter-demo.gif)

> The demo above runs against a real ESXi 8 host. A higher-quality version is in [`docs/media/freecenter-demo.mp4`](docs/media/freecenter-demo.mp4).

## Why

The free ESXi license (vSphere Hypervisor) is a capable hypervisor with a deliberately limited management story:

- every host is managed on its own, through its own Host Client;
- the management API is read-only, so the usual automation tools (Terraform, Ansible modules, PowerCLI scripts) cannot create or change anything;
- there is no vMotion, no cloning, no templates, no backup API and no central inventory.

FreeCenter fills that gap for home labs, small offices and test environments. It reads the inventory through the standard vSphere API and performs every change over SSH with the tools already present on the host, which the license does not restrict. From the outside it behaves like a small vCenter: you point it at your hosts and manage everything from one place, by hand or through automation.

## What you can do with it

### One console for all your hosts

- Register any number of ESXi hosts and browse them in a single inventory, with the familiar four views: hosts, virtual machines, storage, networking.
- Summary pages for hosts, virtual machines, datastores and networks, with capacity and usage at a glance.
- A **Recent Tasks** panel showing every operation, who started it and how it ended, including the ones started through the API or Terraform.
- Search across all hosts.

![Host summary](docs/media/host-summary.png)

### Virtual machines

- **Create** virtual machines with a guided wizard: guest OS, CPU, memory, disks, network, SCSI controller, boot ISO.
- **Deploy from cloud images**: clone a ready-made image (Rocky Linux, Ubuntu, Fedora, Debian: downloadable from a built-in catalog) and let cloud-init set the host name, users, SSH keys and packages on first boot. A new Linux server is ready in about a minute.
- **Edit** everything afterwards: CPU and cores per socket, memory, disks (add, grow, remove, also on other datastores), network adapters, CD/DVD drives, USB controller, firmware (BIOS/EFI), boot delay, CPU and memory hot add, nested virtualization, notes. FreeCenter offers to power the machine off and back on for you when the change requires it.
- **Power** operations: power on and off, reset, suspend, guest shutdown and restart.
- **Web console**: the screen, keyboard and mouse of the machine in your browser, in a floating window you can move and resize while you keep working, or in a tab of its own. Nothing to install, and the browser never needs to reach the host directly.
- **Snapshots**: take, revert, delete, delete all.
- **Clone** a machine, convert it to a **template**, and create new machines from templates.

![Deploying from a cloud image](docs/media/new-vm-cloud-init.png)

![The console of a virtual machine in a floating window](docs/media/vm-console.png)

### Migration between hosts, without vCenter

Move a virtual machine to another host from the **Migrate** dialog. FreeCenter checks compatibility first (CPU, free space, networks) and tells you what will happen:

- a powered-off machine is simply moved;
- between hosts with the same CPU, a running machine is suspended, moved and resumed where it was;
- between hosts with different CPUs, FreeCenter proposes to shut the machine down, move it and start it again on the other side;
- optionally, the disks of a running machine are copied while it keeps running, so the machine is only stopped for the final, short step.

The data travels directly between the two hosts, over a dedicated and restricted channel that FreeCenter sets up for you. If the datastore is shared between the hosts, nothing is copied at all. If anything fails, the machine is brought back on its original host. Details in [docs/migration.md](docs/migration.md).

### Backups and scheduled tasks

- **Back up** a virtual machine while it runs, to any datastore or to another host. A backup is a self-contained, consolidated copy of its disks and configuration.
- **Restore** a backup as a new virtual machine.
- **Schedule** backups and snapshots hourly, daily or weekly, with a retention count: older ones are removed automatically.

### Monitoring

- Real-time charts of CPU, memory, disk and network for hosts and virtual machines, covering the last hour.
- The event log of each host and virtual machine.

![Real-time performance of a host](docs/media/host-monitor.png)

### Host configuration

- **Networking**: virtual switches, port groups and VLANs, physical adapters.
- **Services**: start, stop and restart host services and choose which start with the host.
- **Time**: host clock and NTP servers.
- **Maintenance mode**, reboot and shutdown.
- **Datastore browser**: browse, upload and delete files, download ISO images straight to a datastore from a URL.
- A read-only `esxcli` console for quick diagnostics.

![Virtual switches and port groups](docs/media/host-networks.png)

### Users and security

- Users with two roles: **administrator**, and **read-only** (can see everything, cannot change anything nor open consoles).
- Host credentials are stored encrypted and are never returned by the API.
- The console and all management traffic go through FreeCenter: users only need access to FreeCenter, not to the hosts.

### Automation

- A complete **REST API**: everything the web console does is available to scripts. See [docs/api.md](docs/api.md).
- A **Terraform provider** to describe hosts, networks, virtual machines, templates, snapshots, backups and schedules as code, including migration between hosts by changing a single attribute. See [terraform-provider-freecenter](https://github.com/fdaforno/terraform-provider-freecenter).

```hcl
resource "freecenter_vm" "web" {
  host_id   = data.freecenter_host.main.id   # change it and the VM migrates
  datastore = "datastore1"
  name      = "web-01"
  guest_id  = "rockylinux_64Guest"

  cloud_image = "cloud-images/rocky-9.vmdk"
  num_cpus    = 2
  memory_mb   = 4096
  network     = "VM Network"

  cloud_init {
    hostname            = "web-01"
    ssh_authorized_keys = [file("~/.ssh/id_ed25519.pub")]
    packages            = ["nginx"]
  }
}
```

## A closer look

| | |
|---|---|
| **Add a host**<br>Register an ESXi host with its address and root credentials; its virtual machines, datastores and networks appear immediately. | ![Add a host](docs/media/demo-1-add-host.gif) |
| **Deploy a virtual machine**<br>From a cloud image, customized by cloud-init. | ![Create a VM](docs/media/demo-2-create-vm.gif) |
| **Use its console**<br>Power on, open the console in a floating window, sign in. | ![Console](docs/media/demo-3-console.gif) |
| **Manage it**<br>Snapshots, settings, monitoring, networking. | ![Manage](docs/media/demo-4-manage.gif) |

## Getting started

### Requirements

- One or more hosts running **VMware ESXi 8** (developed and tested on 8.0 U3 with the free license; licensed hosts work too).
- **SSH enabled** on each host (Host Client: *Manage > Services > TSM-SSH > Start*) and the credentials of `root` or an equivalent user.
- A machine that can reach the hosts on ports 443 and 22, with Docker (or Go and Node.js to build from source).

### Run it

```bash
git clone https://github.com/fdaforno/freecenter.git
cd freecenter
docker build -t freecenter .

docker run -d --name freecenter --restart unless-stopped \
  -p 8080:8080 \
  -v freecenter-data:/data \
  -e FREECENTER_ADMIN_PASSWORD='choose-a-password' \
  freecenter
```

Open `http://<your-machine>:8080`, sign in as `admin` with the password you chose, and add your first host: **Add Host**, then its address and credentials. That is all: the inventory fills in by itself.

Keep the `/data` volume safe: it holds the users, the registered hosts with their encrypted credentials, and the encryption key.

### Good to know

- FreeCenter has no HTTPS of its own. To expose it beyond a trusted network, put it behind a reverse proxy that terminates TLS.
- If you start it without `FREECENTER_ADMIN_PASSWORD`, the initial account is `admin` / `admin` and the console insists that you change it.
- More settings, building from source and the internals are described in [docs/development.md](docs/development.md).

## Limitations

FreeCenter works within what a standalone ESXi host can do:

- Changing the hardware of a virtual machine requires it to be powered off (FreeCenter can do the power cycle for you).
- Disks can grow but not shrink, and not while the machine has snapshots.
- Migration of a running machine is not live: there is a short stop while the machine is suspended or restarted. Live vMotion needs vCenter.
- There are no clusters: no HA, no DRS, no distributed switches.
- A machine cannot be moved between two datastores of the same host yet.

## Project layout

| Path | What it is |
|---|---|
| `cmd/`, `internal/` | the FreeCenter server (Go) |
| `web/ui/` | the web console (Angular, [Clarity Design System](https://clarity.design)) |
| `docs/` | API reference, development notes, design notes |

The Terraform provider lives in its own repository: [terraform-provider-freecenter](https://github.com/fdaforno/terraform-provider-freecenter).

## License

[Apache License 2.0](LICENSE).

## Disclaimer

FreeCenter is an independent project. It is not affiliated with, endorsed by or supported by VMware or Broadcom. VMware, ESXi, vSphere, vCenter and vMotion are trademarks of their respective owners. Use it at your own risk, and try it on machines you can afford to lose before trusting it with the ones you cannot.
