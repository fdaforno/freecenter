package esxi

import (
	"freecenter/internal/model"
	"reflect"
	"strings"
	"testing"
)

const extentTable = `Volume Name  VMFS UUID                            Extent Number  Device Name                                                                    Partition
-----------  -----------------------------------  -------------  -----------------------------------------------------------------------------  ---------
datastore1   5f0c1a2b-3c4d5e6f-7a8b-001122334455  0              naa.600508b1001c0a1b                                                           3
my store     5f0c1a2b-3c4d5e6f-7a8b-aabbccddeeff  0              t10.NVMe____Samsung_SSD_990                                                    1
my store     5f0c1a2b-3c4d5e6f-7a8b-aabbccddeeff  1              eui.0025385b71b0c3d4                                                           1
`

func TestParseExtentDevices(t *testing.T) {
	tests := []struct {
		ds   string
		want []string
	}{
		{"datastore1", []string{"naa.600508b1001c0a1b"}},
		{"my store", []string{"eui.0025385b71b0c3d4", "t10.NVMe____Samsung_SSD_990"}},
		{"missing", []string{}},
	}
	for _, tc := range tests {
		got, err := parseExtentDevices(extentTable, tc.ds)
		if err != nil {
			t.Fatalf("%s: %v", tc.ds, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.ds, got, tc.want)
		}
	}
}

func TestParseExtentDevicesCRLFAndEmpty(t *testing.T) {
	got, err := parseExtentDevices(strings.ReplaceAll(extentTable, "\n", "\r\n"), "datastore1")
	if err != nil || len(got) != 1 {
		t.Fatalf("crlf: got %v, %v", got, err)
	}
	if got, err := parseExtentDevices("", "x"); err != nil || len(got) != 0 {
		t.Fatalf("empty: got %v, %v", got, err)
	}
}

func TestParseExtentDevicesBadHeader(t *testing.T) {
	if _, err := parseExtentDevices("Foo  Bar\n---  ---\na    b\n", "a"); err == nil {
		t.Fatal("expected error for unknown header")
	}
}

// Real SATA SMART output (table format).
const sataSmart = `Parameter                          Value  Threshold  Worst  Raw
---------------------------------  -----  ---------  -----  ---
Health Status                      OK     N/A        N/A    N/A
Read Error Count                   75     6          64     95634989
Power-on Hours                     92     0          92     235389977632422
Power Cycle Count                  100    20         100    30
Reallocated Sector Count           100    10         100    0
Drive Temperature                  41     0          47     85899345961
Write Sectors TOT Count            100    0          253    392905282
Read Sectors TOT Count             100    0          253    923530651
Initial Bad Block Count            100    0          100    0
Uncorrectable Error Count          100    0          100    0
Pending Sector Reallocation Count  100    0          100    0
Uncorrectable Sector Count         100    0          100    0
`

func TestParseSmartTable(t *testing.T) {
	h := model.DiskHealth{DeviceID: "t10.ATA_____ST2000DM0082D2FR102"}
	if !parseSmartTable(sataSmart, &h) {
		t.Fatal("expected parseSmartTable to return true")
	}
	if h.SMARTStatus != "OK" {
		t.Errorf("SMARTStatus: got %q, want %q", h.SMARTStatus, "OK")
	}
	if h.ReallocatedSectors != 0 {
		t.Errorf("ReallocatedSectors: got %d, want 0", h.ReallocatedSectors)
	}
	if h.Temperature != 41 || !h.TempOk {
		t.Errorf("Temperature: got %d tempOk=%v, want 41 tempOk=true", h.Temperature, h.TempOk)
	}
	if h.PendingSectors != 0 {
		t.Errorf("PendingSectors: got %d, want 0", h.PendingSectors)
	}
	if h.Uncorrectable != 0 {
		t.Errorf("Uncorrectable: got %d, want 0", h.Uncorrectable)
	}
}

// SATA with reallocated sectors (degraded).
const sataSmartDegraded = `Parameter                          Value  Threshold  Worst  Raw
---------------------------------  -----  ---------  -----  ---
Health Status                      OK     N/A        N/A    N/A
Reallocated Sector Count           99     10         99     1
Drive Temperature                  65     0          24     326418235427
Pending Sector Reallocation Count  100    0          100    1
Uncorrectable Sector Count         100    0          100    0
`

func TestParseSmartTableDegraded(t *testing.T) {
	h := model.DiskHealth{DeviceID: "t10.ATA_____CT480BX500SSD1"}
	parseSmartTable(sataSmartDegraded, &h)
	if h.ReallocatedSectors != 1 {
		t.Errorf("ReallocatedSectors: got %d, want 1", h.ReallocatedSectors)
	}
	if h.PendingSectors != 1 {
		t.Errorf("PendingSectors: got %d, want 1", h.PendingSectors)
	}
	if h.Temperature != 65 {
		t.Errorf("Temperature: got %d, want 65", h.Temperature)
	}
}

// Real NVMe SMART output (key:value, temperature in Kelvin).
const nvmeSmart = `SMART And Health Info:
   Available Spare Space Below Threshold: false
   Temperature Warning: false
   NVM Subsystem Reliability Degradation: false
   Read Only Mode: false
   Volatile Memory Backup Device Failure: false
   Composite Temperature: 328 K
   Available Spare: 100 %
   Available Spare Threshold: 0 %
   Percentage Used: 14 %
   Data Units Read: 0x1586929
   Data Units Written: 0x46c35af
   Host Read Commands: 0x7295a38
   Host Write Commands: 0x27b15b67
   Controller Busy Time: 0x1a2
   Power Cycles: 0x2b
   Power On Hours: 0x8e5d
   Unsafe Shutdowns: 0x18
   Media Errors: 0x0
   Number of Error Info Log Entries: 0x0
   Warning Composite Temperature Time: 0 Mins
   Critical Composite Temperature Time: 0 Mins
   Temperature Sensor 1: 0 K
   Temperature Sensor 2: 0 K
   Temperature Sensor 3: 0 K
   Temperature Sensor 4: 0 K
   Temperature Sensor 5: 0 K
   Temperature Sensor 6: 0 K
   Temperature Sensor 7: 0 K
   Temperature Sensor 8: 0 K
`

func TestParseNVMeSmart(t *testing.T) {
	h := model.DiskHealth{DeviceID: "t10.NVMe____KCM6DRUL7T68"}
	if !parseNVMeSmart(nvmeSmart, &h) {
		t.Fatal("expected parseNVMeSmart to return true")
	}
	if h.SMARTStatus != "OK" {
		t.Errorf("SMARTStatus: got %q, want %q", h.SMARTStatus, "OK")
	}
	// 328 K - 273 = 55 C
	if h.Temperature != 55 || !h.TempOk {
		t.Errorf("Temperature: got %d tempOk=%v, want 55 tempOk=true", h.Temperature, h.TempOk)
	}
	if h.PercentageUsed != 14 {
		t.Errorf("PercentageUsed: got %d, want 14", h.PercentageUsed)
	}
	if h.MediaErrors != 0 {
		t.Errorf("MediaErrors: got %d, want 0", h.MediaErrors)
	}
}

// NVMe with degradation.
const nvmeSmartDegraded = `SMART And Health Info:
   Available Spare Space Below Threshold: true
   NVM Subsystem Reliability Degradation: true
   Composite Temperature: 338 K
   Percentage Used: 99 %
   Media Errors: 0x5
`

func TestParseNVMeSmartDegraded(t *testing.T) {
	h := model.DiskHealth{}
	parseNVMeSmart(nvmeSmartDegraded, &h)
	if h.SMARTStatus != "Degraded" {
		t.Errorf("SMARTStatus: got %q, want %q", h.SMARTStatus, "Degraded")
	}
	// 338 K - 273 = 65 C
	if h.Temperature != 65 {
		t.Errorf("Temperature: got %d, want 65", h.Temperature)
	}
	if h.PercentageUsed != 99 {
		t.Errorf("PercentageUsed: got %d, want 99", h.PercentageUsed)
	}
	if h.MediaErrors != 5 {
		t.Errorf("MediaErrors: got %d, want 5", h.MediaErrors)
	}
}
