# Changelog

All notable changes to FreeCenter are listed here. Dates are in UTC.

## [Unreleased]

### Added

- **Disk health (SMART/NVMe).** The datastore Summary now shows a Disk
  Health card with the SMART/NVMe status of every device backing the
  datastore: overall health, reallocated sectors, pending sectors,
  uncorrectable sectors, media errors (NVMe), endurance used (NVMe) and
  temperature. Values that may compromise the drive — reallocated
  sectors > 0, SMART not OK, temperature at or above 65 °C — are shown
  in red; the warning band (50–65 °C, endurance ≥ 80 %) is yellow.
  Health is read over SSH with `esxcli storage core device smart get`
  (SATA/SAS) and `esxcli nvme device log smart get` (NVMe, with
  Kelvin→Celsius conversion and device-to-HBA mapping).
- **Alarms.** The root Summary gains an Alarms card that aggregates
  active disk-health conditions across all connected hosts. Alarms are
  probed every 10 minutes in the background; a manual Refresh button
  triggers an immediate probe.
- **New API endpoints.**
  - `GET /hosts/{hid}/datastores/{ds}/health` — cached disk health of
    one datastore.
  - `GET /alarms` — cached alarm list across all hosts.
  - `POST /alarms/refresh` — trigger an immediate probe and return the
    fresh alarms.
- **esxcli allow list.** `esxcli storage core device list`,
  `esxcli storage core device smart`, `esxcli nvme device list`,
  `esxcli nvme device log` and `esxcli nvme device namespace` are now
  permitted in the read-only esxcli console.

## 2026-10-07

### Added

- **Datastore browser**: register a virtual machine that already exists
  on a datastore from its `.vmx` file, and download files from a
  datastore.

## 2026-10-06

### Added

- Initial public release of FreeCenter: a vCenter-style management
  console for the free VMware ESXi hypervisor. One web console for all
  hosts, virtual machine lifecycle, templates and clones, migration
  between hosts, backups, scheduled tasks, monitoring, users and roles,
  a REST API and a Terraform provider.
