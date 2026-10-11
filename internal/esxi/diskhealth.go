package esxi

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"hostwarden/internal/model"
)

// Disk health is read over SSH: a standalone host does not expose
// SMART/NVMe health through the SOAP API.
//
// SATA/SAS: "esxcli storage core device smart get -d <id>" produces a
// table (Parameter / Value / Threshold / Worst / Raw).
//
// NVMe: "esxcli nvme device log smart get -A vmhbaX" produces key:value
// pairs with temperature in Kelvin. We map device IDs to HBA names via
// "esxcli nvme device namespace list -A vmhbaX".

const diskHealthTimeout = 90 * time.Second

// DatastoreHealth returns the health of every device backing a datastore.
func (c *Client) DatastoreHealth(ctx context.Context, datastore string) (*model.DatastoreHealth, error) {
	if c.ssh == nil {
		return nil, ErrSSHDisabled
	}
	devices, err := c.datastoreDevices(ctx, datastore)
	if err != nil {
		return nil, err
	}

	// Build NVMe device-id → HBA-name map once, for all NVMe devices.
	nvmeMap, _ := c.nvmeDeviceMap(ctx)

	out := &model.DatastoreHealth{Datastore: datastore, HostID: c.rec.ID, Devices: []model.DiskHealth{}}
	for _, id := range devices {
		out.Devices = append(out.Devices, c.probeDevice(ctx, id, nvmeMap))
	}
	return out, nil
}

// datastoreDevices maps a datastore name to its backing device IDs via
// "esxcli storage vmfs extent list". Column positions are determined from
// the dashed separator line so multi-word headers like "Volume Name" and
// "Device Name" are handled correctly.
func (c *Client) datastoreDevices(ctx context.Context, datastore string) ([]string, error) {
	out, err := c.sshRun(ctx, "esxcli storage vmfs extent list")
	if err != nil {
		return nil, fmt.Errorf("list vmfs extents: %s", remoteFailure(err, out))
	}
	return parseExtentDevices(out, datastore)
}

// parseExtentDevices extracts the sorted, de-duplicated device IDs of the
// extents belonging to datastore from "esxcli storage vmfs extent list".
func parseExtentDevices(out, datastore string) ([]string, error) {
	lines := strings.Split(strings.ReplaceAll(out, "\r", ""), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) < 2 {
		return []string{}, nil
	}
	header := lines[0]
	cols := dashColumns(lines[1])
	if len(cols) == 0 {
		return nil, fmt.Errorf("vmfs extent list: unexpected header %q", header)
	}
	dsCol, deviceCol := -1, -1
	for j, span := range cols {
		switch strings.ToLower(strings.TrimSpace(sliceCols(header, span))) {
		case "volume name":
			dsCol = j
		case "device name":
			deviceCol = j
		}
	}
	if dsCol < 0 || deviceCol < 0 {
		return nil, fmt.Errorf("vmfs extent list: unexpected header %q", header)
	}
	seen := map[string]bool{}
	devices := []string{}
	for _, line := range lines[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.TrimSpace(sliceCols(line, cols[dsCol])) != datastore {
			continue
		}
		id := strings.TrimSpace(sliceCols(line, cols[deviceCol]))
		if id != "" && !seen[id] {
			seen[id] = true
			devices = append(devices, id)
		}
	}
	sort.Strings(devices)
	return devices, nil
}

// dashColumns returns the [start, end) offsets of each run of '-' in an
// esxcli table separator line. The last column is open-ended (end = -1).
func dashColumns(sep string) [][2]int {
	var cols [][2]int
	start := -1
	for i := 0; i <= len(sep); i++ {
		dash := i < len(sep) && sep[i] == '-'
		switch {
		case dash && start < 0:
			start = i
		case !dash && start >= 0:
			cols = append(cols, [2]int{start, i})
			start = -1
		}
	}
	if n := len(cols); n > 0 {
		cols[n-1][1] = -1
	}
	return cols
}

// sliceCols returns line[span[0]:span[1]], clamped to the line length.
func sliceCols(line string, span [2]int) string {
	if span[0] >= len(line) {
		return ""
	}
	end := span[1]
	if end < 0 || end > len(line) {
		end = len(line)
	}
	return line[span[0]:end]
}

// nvmeDeviceMap returns a map of NVMe device ID → HBA name by iterating
// "esxcli nvme device list" and "esxcli nvme device namespace list -A <hba>".
func (c *Client) nvmeDeviceMap(ctx context.Context) (map[string]string, error) {
	out, err := c.sshRun(ctx, "esxcli nvme device list")
	if err != nil {
		return nil, err
	}
	hbas := []string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "vmhba") {
			hbas = append(hbas, fields[0])
		}
	}
	m := map[string]string{}
	for _, hba := range hbas {
		nsOut, err := c.sshRun(ctx, "esxcli nvme device namespace list -A "+shq(hba))
		if err != nil {
			continue
		}
		// The output has a header row, a dashed separator, then data rows
		// with "Namespace ID  Status  Device Name".
		lines := strings.Split(strings.ReplaceAll(nsOut, "\r", ""), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 3 && strings.HasPrefix(fields[len(fields)-1], "t10.") {
				m[fields[len(fields)-1]] = hba
			}
		}
	}
	return m, nil
}

// probeDevice reads what SMART/NVMe health is available for one device.
func (c *Client) probeDevice(ctx context.Context, id string, nvmeMap map[string]string) model.DiskHealth {
	h := model.DiskHealth{DeviceID: id}

	// Universal info: display name and form factor.
	if out, err := c.sshRun(ctx, "esxcli storage core device list -d "+shq(id)); err == nil {
		parseDeviceList(out, &h)
	} else {
		h.Error = remoteFailure(err, out)
	}

	// Try SATA/SAS SMART first.
	if out, err := c.sshRun(ctx, "esxcli storage core device smart get -d "+shq(id)); err == nil {
		if parseSmartTable(out, &h) {
			return h
		}
	}

	// NVMe fallback: use HBA name to read the SMART log page.
	if hba, ok := nvmeMap[id]; ok {
		if out, err := c.sshRun(ctx, "esxcli nvme device log smart get -A "+shq(hba)); err == nil {
			if parseNVMeSmart(out, &h) {
				return h
			}
		}
	}

	h.Unsupported = true
	return h
}

// parseDeviceList fills display name and form factor from
// "esxcli storage core device list -d <id>".
func parseDeviceList(out string, h *model.DiskHealth) {
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch strings.ToLower(k) {
		case "display name":
			h.DisplayName = v
		case "is ssd":
			if v == "true" {
				h.FormFactor = "SSD"
			} else if v == "false" && h.FormFactor == "" {
				h.FormFactor = "HDD"
			}
		case "is sas":
			if v == "true" {
				h.FormFactor = "SAS"
			}
		}
	}
}

// parseSmartTable fills h from the tabular output of
// "esxcli storage core device smart get -d <id>":
//
//	Parameter                          Value  Threshold  Worst  Raw
//	---------------------------------  -----  ---------  -----  ---
//	Health Status                      OK     N/A        N/A    N/A
//	Reallocated Sector Count           100    10         100    0
//	Drive Temperature                  41     0          47     85899345961
//	...
//
// Returns true when at least one recognized field was found.
func parseSmartTable(out string, h *model.DiskHealth) bool {
	saw := false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.Join(fields[:len(fields)-4], " ")
		value := fields[len(fields)-4]
		raw := fields[len(fields)-1]
		switch strings.ToLower(name) {
		case "health status":
			saw = true
			h.SMARTStatus = value
		case "reallocated sector count":
			saw = true
			h.ReallocatedSectors = atoiOrZero(raw)
		case "pending sector reallocation count":
			saw = true
			h.PendingSectors = atoiOrZero(raw)
		case "uncorrectable sector count", "uncorrectable error count":
			saw = true
			h.Uncorrectable = atoiOrZero(raw)
		case "drive temperature":
			saw = true
			h.Temperature = atoiOrZero(value)
			h.TempOk = true
		}
	}
	return saw
}

// parseNVMeSmart fills h from "esxcli nvme device log smart get -A <hba>":
//
//	SMART And Health Info:
//	   Available Spare Space Below Threshold: false
//	   NVM Subsystem Reliability Degradation: false
//	   Composite Temperature: 328 K
//	   Available Spare: 100 %
//	   Percentage Used: 14 %
//	   Media Errors: 0x0
//	   ...
//
// Temperature is in Kelvin and converted to Celsius. Returns true when
// at least one recognized field was found.
func parseNVMeSmart(out string, h *model.DiskHealth) bool {
	saw := false
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "nvm subsystem reliability degradation":
			saw = true
			if v == "true" {
				h.SMARTStatus = "Degraded"
			} else if h.SMARTStatus == "" {
				h.SMARTStatus = "OK"
			}
		case "available spare space below threshold":
			saw = true
			if v == "true" && h.SMARTStatus == "" {
				h.SMARTStatus = "Warning"
			}
		case "composite temperature":
			saw = true
			// Kelvin → Celsius
			if kelvin, err := strconv.Atoi(strings.TrimSuffix(v, " K")); err == nil {
				h.Temperature = kelvin - 273
				h.TempOk = true
			}
		case "percentage used":
			saw = true
			h.PercentageUsed = atoiOrZero(strings.TrimSuffix(v, "%"))
		case "media errors":
			saw = true
			// value is hex like "0x0"
			if n, err := strconv.ParseInt(v, 0, 64); err == nil {
				h.MediaErrors = int(n)
			}
		}
	}
	return saw
}

func atoiOrZero(v string) int {
	v = strings.TrimSpace(v)
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}
