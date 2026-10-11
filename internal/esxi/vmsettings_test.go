package esxi

import (
	"errors"
	"strings"
	"testing"

	"hostwarden/internal/model"
)

func ptr[T any](v T) *T { return &v }

const settingsVMX = `displayName = "old"
guestOS = "ubuntu-64"
ethernet0.present = "TRUE"
ethernet0.networkName = "VM Network"
ethernet0.generatedAddress = "00:0c:29:aa:bb:cc"
ide1:0.present = "TRUE"
ide1:0.deviceType = "cdrom-image"
ide1:0.fileName = "/vmfs/volumes/ds/ISO/a.iso"
usb.present = "TRUE"
ehci.present = "TRUE"
`

func settingsAddrs() map[int32]deviceAddr {
	return map[int32]deviceAddr{
		4000: {key: 4000, mac: "00:0C:29:AA:BB:CC"},
		3002: {key: 3002, ctrl: "ide", bus: 1, unit: 0},
	}
}

func wantVMX(t *testing.T, doc *vmxDoc, key, want string) {
	t.Helper()
	if got, _ := doc.get(key); got != want {
		t.Errorf("%s = %q, want %q", key, got, want)
	}
}

func TestApplyVMSettingsOptions(t *testing.T) {
	doc := parseVMX(settingsVMX)
	req := &model.UpdateVMRequest{
		Name:           ptr("new name"),
		GuestID:        ptr("rockylinux_64Guest"),
		Firmware:       ptr("efi"),
		BootDelayMs:    ptr(int64(3000)),
		EnterBIOSSetup: ptr(true),
		CPUHotAdd:      ptr(true),
		NestedHV:       ptr(false),
		USBController:  ptr("usb3"),
	}
	changed, err := applyVMSettings(doc, req, settingsAddrs(), false)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	wantVMX(t, doc, "displayName", "new name")
	wantVMX(t, doc, "guestOS", "rockylinux-64")
	wantVMX(t, doc, "firmware", "efi")
	wantVMX(t, doc, "bios.bootDelay", "3000")
	wantVMX(t, doc, "bios.forceSetupOnce", "TRUE")
	wantVMX(t, doc, "vcpu.hotadd", "TRUE")
	wantVMX(t, doc, "vhv.enable", "FALSE")
	wantVMX(t, doc, "usb_xhci.present", "TRUE")
	if _, ok := doc.get("usb.present"); ok {
		t.Error("usb 2.0 controller should have been replaced")
	}
	if _, ok := doc.get("mem.hotadd"); ok {
		t.Error("unset option must not be written")
	}
}

func TestApplyVMSettingsDevices(t *testing.T) {
	doc := parseVMX(settingsVMX)
	req := &model.UpdateVMRequest{
		UpdateNICs:   []model.NICUpdate{{Key: 4000, Network: ptr("DMZ"), StartConnected: ptr(false)}},
		UpdateCdroms: []model.CdromUpdate{{Key: 3002, ISOPath: ""}},
		AddCdroms:    []model.CdromSpec{{ISOPath: "[ds] ISO/b.iso"}},
	}
	if _, err := applyVMSettings(doc, req, settingsAddrs(), false); err != nil {
		t.Fatal(err)
	}
	wantVMX(t, doc, "ethernet0.networkName", "DMZ")
	wantVMX(t, doc, "ethernet0.startConnected", "FALSE")
	// existing drive switched to client device
	wantVMX(t, doc, "ide1:0.deviceType", "atapi-cdrom")
	wantVMX(t, doc, "ide1:0.clientDevice", "TRUE")
	// new drive takes the first free IDE slot
	wantVMX(t, doc, "ide0:0.deviceType", "cdrom-image")
	wantVMX(t, doc, "ide0:0.fileName", "/vmfs/volumes/ds/ISO/b.iso")

	doc = parseVMX(settingsVMX)
	if _, err := applyVMSettings(doc, &model.UpdateVMRequest{RemoveCdromKeys: []int32{3002}}, settingsAddrs(), false); err != nil {
		t.Fatal(err)
	}
	if len(doc.keysWithPrefix("ide1:0")) != 0 {
		t.Error("removed cd/dvd drive still present")
	}
}

func TestApplyVMSettingsRejections(t *testing.T) {
	doc := parseVMX(settingsVMX)
	before := doc.String()

	changed, err := applyVMSettings(doc, &model.UpdateVMRequest{}, settingsAddrs(), true)
	if err != nil || changed {
		t.Fatalf("empty request on a running VM: changed=%v err=%v", changed, err)
	}
	if _, err := applyVMSettings(doc, &model.UpdateVMRequest{Name: ptr("x")}, settingsAddrs(), true); !errors.Is(err, ErrInvalidState) {
		t.Errorf("powered-on change: err=%v, want ErrInvalidState", err)
	}
	if doc.String() != before {
		t.Error("vmx modified by a rejected request")
	}
	if _, err := applyVMSettings(doc, &model.UpdateVMRequest{UpdateCdroms: []model.CdromUpdate{{Key: 9}}}, settingsAddrs(), false); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown cd/dvd key: err=%v, want ErrNotFound", err)
	}
}

func TestFreeIDESlot(t *testing.T) {
	doc := parseVMX("ide0:0.present = \"TRUE\"\nide0:1.present = \"TRUE\"\nide1:0.present = \"TRUE\"\n")
	if got, ok := freeIDESlot(doc); !ok || got != "ide1:1" {
		t.Errorf("got %q %v, want ide1:1", got, ok)
	}
	doc.set("ide1:1.present", "TRUE")
	if _, ok := freeIDESlot(doc); ok {
		t.Error("expected no free slot")
	}
}

func TestEthernetIndex(t *testing.T) {
	doc := parseVMX("ethernet0.present = \"TRUE\"\nethernet1.present = \"TRUE\"\nethernet1.generatedAddress = \"00:0c:29:11:22:33\"\n")
	addrs := map[int32]deviceAddr{
		4000: {key: 4000},                           // never powered on: no MAC yet
		4005: {key: 4005, mac: "00:0C:29:11:22:33"}, // found by MAC regardless of key
		4003: {key: 4003},
	}
	for key, want := range map[int32]int{4000: 0, 4005: 1} {
		if got, err := ethernetIndex(doc, addrs, key); err != nil || got != want {
			t.Errorf("key %d: got %d, %v; want %d", key, got, err, want)
		}
	}
	if _, err := ethernetIndex(doc, addrs, 4003); err == nil {
		t.Error("key 4003 has no VMX entry: expected an error")
	}
	if _, err := ethernetIndex(doc, addrs, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown key: err=%v, want ErrNotFound", err)
	}
}

func TestBuildUserData(t *testing.T) {
	got := buildUserData(&model.CloudInitConfig{
		Hostname:     "web-01",
		RootPassword: "r00t",
		Users:        []model.CloudInitUser{{Name: "ops", Sudo: "ALL=(ALL) NOPASSWD:ALL", PlainPassword: "p: #1"}},
		RunCmd:       []string{"echo done"},
	})
	for _, want := range []string{
		`hostname: "web-01"`,
		`plain_text_passwd: "p: #1"`, // quoted: ':' and '#' must not break the YAML
		"ssh_pwauth: true",
		`  - "echo done"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("user-data lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "runcmd:"); n != 1 {
		t.Errorf("runcmd appears %d times, want 1:\n%s", n, got)
	}
}
