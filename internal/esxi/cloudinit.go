package esxi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"freecenter/internal/model"
)

// CloudImageCatalog is the list of known cloud images that can be downloaded.
var CloudImageCatalog = []struct {
	Distro  string `json:"distro"`
	Ver     string `json:"ver"`
	Variant string `json:"variant"`
	Size    string `json:"size"`
	URL     string `json:"url"`
}{
	{"Rocky Linux", "9", "GenericCloud", "~0.9 GB", "https://download.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud.latest.x86_64.qcow2"},
	{"Ubuntu", "24.04 LTS", "Server", "~0.6 GB", "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-amd64.img"},
	{"Ubuntu", "22.04 LTS", "Server", "~0.6 GB", "https://cloud-images.ubuntu.com/releases/22.04/release/ubuntu-22.04-server-cloudimg-amd64.img"},
	{"Fedora", "40", "Cloud Base", "~0.5 GB", "https://download.fedoraproject.org/pub/fedora/linux/releases/40/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-40-1.14.x86_64.qcow2"},
	{"Debian", "12", "GenericCloud", "~0.5 GB", "https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2"},
}

// DownloadCloudImage downloads a qcow2 cloud image, converts it to VMDK
// (streamOptimized, ESXi-compatible) and uploads it to the datastore.
// cloudPath is relative to the datastore root (e.g. "cloud-images/rocky-9.vmdk").
func (c *Client) DownloadCloudImage(ctx context.Context, datastore, cloudPath, url string) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	if strings.Contains(cloudPath, "..") {
		return fmt.Errorf("invalid cloud path")
	}
	// download qcow2
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, resp.Status)
	}
	tmpQcow2, err := os.CreateTemp("", "fc-cloud-*.qcow2")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmpQcow2.Name())
	if _, err := copyAndClose(resp.Body, tmpQcow2); err != nil {
		return fmt.Errorf("download write: %w", err)
	}
	// convert to VMDK streamOptimized (ESXi-compatible single file)
	tmpVmdk, err := os.CreateTemp("", "fc-cloud-*.vmdk")
	if err != nil {
		return fmt.Errorf("temp vmdk: %w", err)
	}
	defer os.Remove(tmpVmdk.Name())
	tmpVmdk.Close()
	conv := exec.CommandContext(ctx, "qemu-img", "convert", "-f", "qcow2", "-O", "vmdk", "-o", "subformat=streamOptimized", tmpQcow2.Name(), tmpVmdk.Name())
	if out, err := conv.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img convert: %w (output: %s)", err, out)
	}
	// upload VMDK to datastore via SSH
	dsPath := "/vmfs/volumes/" + datastore + "/" + strings.TrimPrefix(cloudPath, "/")
	dir := filepath.Dir(dsPath)
	if out, err := c.sshRun(ctx, "mkdir -p "+shq(dir)); err != nil {
		return fmt.Errorf("mkdir: %w (output: %s)", err, out)
	}
	if err := c.sshPut(ctx, "cat > "+shq(dsPath), mustOpen(tmpVmdk.Name())); err != nil {
		return fmt.Errorf("ssh upload vmdk: %w", err)
	}
	return nil
}

func copyAndClose(r interface{ Read([]byte) (int, error) }, f *os.File) (int64, error) {
	defer f.Close()
	var buf [65536]byte
	var total int64
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			wn, werr := f.Write(buf[:n])
			total += int64(wn)
			if werr != nil {
				return total, werr
			}
		}
		if err != nil {
			if err.Error() == "EOF" {
				return total, nil
			}
			return total, err
		}
	}
}

func mustOpen(p string) *os.File {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	return f
}

// BuildSeedISO creates a cloud-init NoCloud seed ISO from the given config.
// Returns the path to the temporary ISO file (caller must remove it).
func BuildSeedISO(cfg *model.CloudInitConfig, vmName string) (string, error) {
	dir, err := os.MkdirTemp("", "fc-seed-*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	// meta-data
	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", vmName, vmName)
	if cfg.Hostname != "" {
		metaData = fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", vmName, cfg.Hostname)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta-data"), []byte(metaData), 0644); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	// user-data
	userData := cfg.UserData
	if userData == "" {
		userData = buildUserData(cfg)
	}
	if err := os.WriteFile(filepath.Join(dir, "user-data"), []byte(userData), 0644); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	// network-config (optional)
	if cfg.NetworkConfig != "" {
		if err := os.WriteFile(filepath.Join(dir, "network-config"), []byte(cfg.NetworkConfig), 0644); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	// build ISO with genisoimage
	isoPath := dir + ".iso"
	args := []string{"-output", isoPath, "-volid", "cidata", "-joliet", "-rock"}
	if cfg.NetworkConfig != "" {
		args = append(args, filepath.Join(dir, "user-data"), filepath.Join(dir, "meta-data"), filepath.Join(dir, "network-config"))
	} else {
		args = append(args, filepath.Join(dir, "user-data"), filepath.Join(dir, "meta-data"))
	}
	cmd := exec.Command("genisoimage", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		os.Remove(isoPath)
		return "", fmt.Errorf("genisoimage: %w (output: %s)", err, out)
	}
	os.RemoveAll(dir)
	return isoPath, nil
}

func buildUserData(cfg *model.CloudInitConfig) string {
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	// One runcmd list: YAML does not allow the key twice.
	var runCmds []string
	if cfg.Hostname != "" {
		b.WriteString("hostname: " + yamlString(cfg.Hostname) + "\n")
	}
	passwordLogin := cfg.RootPassword != ""
	for _, u := range cfg.Users {
		passwordLogin = passwordLogin || u.PlainPassword != ""
	}
	if passwordLogin {
		b.WriteString("ssh_pwauth: true\n")
	}
	if cfg.RootPassword != "" {
		b.WriteString("chpasswd:\n  list: |\n    root:" + cfg.RootPassword + "\n  expire: false\n")
		runCmds = append(runCmds,
			`sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config`,
			`sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config`,
			"systemctl restart sshd",
		)
	}
	if len(cfg.SSHAuthorizedKeys) > 0 {
		b.WriteString("ssh_authorized_keys:\n")
		for _, k := range cfg.SSHAuthorizedKeys {
			b.WriteString("  - " + k + "\n")
		}
	}
	if len(cfg.Users) > 0 {
		b.WriteString("users:\n")
		for _, u := range cfg.Users {
			b.WriteString("  - name: " + yamlString(u.Name) + "\n")
			if u.Sudo != "" {
				b.WriteString("    sudo: " + u.Sudo + "\n")
			}
			if u.Shell != "" {
				b.WriteString("    shell: " + u.Shell + "\n")
			}
			if u.PlainPassword != "" {
				b.WriteString("    plain_text_passwd: " + yamlString(u.PlainPassword) + "\n")
				b.WriteString("    lock_passwd: false\n")
			}
			if len(u.SSHAuthorizedKeys) > 0 {
				b.WriteString("    ssh_authorized_keys:\n")
				for _, k := range u.SSHAuthorizedKeys {
					b.WriteString("      - " + k + "\n")
				}
			}
		}
	}
	if len(cfg.Packages) > 0 {
		b.WriteString("packages:\n")
		for _, p := range cfg.Packages {
			b.WriteString("  - " + p + "\n")
		}
	}
	if len(cfg.WriteFiles) > 0 {
		b.WriteString("write_files:\n")
		for _, f := range cfg.WriteFiles {
			b.WriteString("  - path: " + f.Path + "\n")
			if f.Permissions != "" {
				b.WriteString("    permissions: '" + f.Permissions + "'\n")
			}
			if f.Owner != "" {
				b.WriteString("    owner: " + f.Owner + "\n")
			}
			b.WriteString("    content: |\n")
			for _, line := range strings.Split(f.Content, "\n") {
				b.WriteString("      " + line + "\n")
			}
		}
	}
	runCmds = append(runCmds, cfg.RunCmd...)
	if len(runCmds) > 0 {
		b.WriteString("runcmd:\n")
		for _, c := range runCmds {
			b.WriteString("  - " + yamlString(c) + "\n")
		}
	}
	return b.String()
}

// yamlString renders s as a double-quoted YAML scalar (JSON string syntax is
// valid YAML), so values with ':' or '#' cannot break or alter the document.
func yamlString(s string) string {
	q, _ := json.Marshal(s)
	return string(q)
}
