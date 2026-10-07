# Development and internals

## How it works

The free ESXi license makes the vSphere management API **read-only**: any call that changes something fails with `RestrictedVersion`. Reads work normally. FreeCenter therefore uses two channels to each host:

- the **vSphere API** (port 443, through [govmomi](https://github.com/vmware/govmomi)) for everything it reads: inventory, host status, datastores, networks, performance, events, console tickets, file transfers;
- **SSH** (port 22) for everything it changes, using the tools that ship with ESXi (`vim-cmd`, `vmkfstools`, `esxcli`) and by editing virtual machine configuration files.

Nothing is installed on the hosts. For migrations and remote backups FreeCenter creates a dedicated SSH key on the source host and authorizes it on the target for the source address only, so that the two hosts exchange data directly instead of through the FreeCenter server.

The console of a virtual machine is the host's WebMKS stream (RFB over WebSocket), relayed by the FreeCenter server to [noVNC](https://novnc.com) in the browser: the browser never talks to the host and does not need to trust its certificate.

The server keeps its own state (users, registered hosts, schedules) in a single JSON file. Host passwords in it are encrypted with AES-256-GCM. Recent Tasks and background jobs are kept in memory and start empty after a restart.

How migration works in detail is described in [migration.md](migration.md).

## Repository layout

| Path | Content |
|---|---|
| `cmd/freecenter` | server entry point |
| `internal/esxi` | everything that talks to a host |
| `internal/api` | REST API, inventory cache, task log, background jobs, scheduler |
| `internal/store` | persistent state and its encryption |
| `web/ui` | web console (Angular + Clarity); its build in `web/dist` is embedded in the server binary |

The Terraform provider is a separate project: [terraform-provider-freecenter](https://github.com/fdaforno/terraform-provider-freecenter).

## Build and run

Requirements: Go 1.26+, Node.js 22+ (for the web console), Docker for the image.

```bash
make run          # build the web console and start the server on http://localhost:8080 (data in ./data)
make ui-dev       # optional: web console with hot reload on :4200, /api forwarded to :8080
make build        # server binary with the web console embedded
make vet test     # go vet and tests
make ui-lint      # lint of the web console
docker build -t freecenter .
```

Creating virtual machines with cloud-init needs `genisoimage` on the machine running the server. The Docker image includes it.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `FREECENTER_ADDR` | `:8080` | listen address |
| `FREECENTER_DATA` | `./data` (`/data` in the image) | state directory |
| `FREECENTER_ADMIN_USER` | `admin` | initial administrator, created on first start |
| `FREECENTER_ADMIN_PASSWORD` | `admin` | its password; with the default the console insists on a change |
| `FREECENTER_SECRET_KEY` | (file `secret.key` in the state directory) | key that encrypts host passwords |

**About the key.** By default the key is generated on first start and stored next to the state file: a copy of the state file alone is useless, but whoever can read the whole directory has both. To keep them apart, provide the key through `FREECENTER_SECRET_KEY`. Without the right key the server refuses to start.

## Notes on behavior

- Hardware changes and deletion require the virtual machine to be powered off.
- Disks can only grow, and not while snapshots exist.
- The inventory is cached by the server and refreshed in the background; every change invalidates the cache of the host involved.
- The `esxcli` console only accepts read-only commands from an allow list.
