package esxi

import "testing"

func TestCommandAllowed(t *testing.T) {
	allowed := []string{
		"esxcli system version get",
		"  esxcli storage filesystem list  ",
		"esxcli system settings advanced list -o /Net/FollowHardwareMac",
		"esxcli storage core device smart get -d naa.600508b1001c4d41",
		"esxcli storage core device smart get --device-name=t10.ATA_____Samsung_SSD_870",
		"esxcli nvme device log smart get -A vmhba2",
		"esxcli network ip interface ipv4 get",
		"esxcli network vswitch standard portgroup list",
		"esxcli vm process list",
		"vim-cmd vimsvc/license --show",
		"vim-cmd vmsvc/getallvms",
	}
	for _, cmd := range allowed {
		if !CommandAllowed(cmd) {
			t.Errorf("CommandAllowed(%q) = false, want true", cmd)
		}
	}

	refused := []string{
		"",
		"esxcli",
		"esxcli system version",
		"esxcli software vib list",
		// commands that change the host
		"esxcli vm process kill --type=force --world-id=1234",
		"esxcli system settings advanced set -o /Net/FollowHardwareMac -i 1",
		"esxcli network vswitch standard add -v vSwitch9",
		"esxcli system version get set",
		"vim-cmd vimsvc/license --set=00000-00000-00000-00000-00000",
		"vim-cmd vimsvc/license",
		"vim-cmd vmsvc/getallvms extra",
		"vim-cmd vmsvc/power.off 1",
		// anything the remote shell would interpret
		"esxcli system version get; reboot",
		"esxcli system version get && reboot",
		"esxcli system version get | sh",
		"esxcli system version get > /etc/passwd",
		"esxcli system version get $(reboot)",
		"esxcli system version get `reboot`",
		"esxcli system version get\nreboot",
		"esxcli system version get -o 'a b'",
		"esxcli\tsystem version get",
		"rm -rf /vmfs/volumes/datastore1",
	}
	for _, cmd := range refused {
		if CommandAllowed(cmd) {
			t.Errorf("CommandAllowed(%q) = true, want false", cmd)
		}
	}
}
