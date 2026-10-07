package esxi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

// UploadDatastoreFile uploads a local file to [datastore] remotePath.
func (c *Client) UploadDatastoreFile(ctx context.Context, datastore, remotePath, localFile string) error {
	if err := c.Ensure(ctx); err != nil {
		return err
	}
	ds, err := c.finder.Datastore(ctx, datastore)
	if err != nil {
		return err
	}
	remotePath = strings.TrimPrefix(remotePath, "/")
	return ds.UploadFile(ctx, localFile, ds.Path(remotePath), nil)
}

// DownloadDatastoreFile opens a file of a datastore for reading and returns
// its content and size. The caller closes the reader.
func (c *Client) DownloadDatastoreFile(ctx context.Context, datastore, remotePath string) (io.ReadCloser, int64, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, 0, err
	}
	ds, err := c.finder.Datastore(ctx, datastore)
	if err != nil {
		return nil, 0, err
	}
	return ds.Download(ctx, strings.TrimPrefix(remotePath, "/"), nil)
}

// DeleteDatastoreFile deletes a file or an empty directory on a datastore.
// Directories are never removed recursively: a folder with content is refused.
// Uses SSH (rm/rmdir) because the FileManager API task is license-gated.
func (c *Client) DeleteDatastoreFile(ctx context.Context, datastore, remotePath string) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	remotePath = strings.TrimPrefix(remotePath, "/")
	if remotePath == "" || strings.Contains(remotePath, "..") {
		return fmt.Errorf("invalid path")
	}
	fsPath := "/vmfs/volumes/" + datastore + "/" + remotePath
	q := shq(fsPath)
	cmd := fmt.Sprintf("if [ -d %s ]; then rmdir %s; else rm -f %s; fi", q, q, q)
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return fmt.Errorf("delete %s: %w (output: %s)", remotePath, err, out)
	}
	return nil
}

// DownloadISO downloads a URL to a datastore folder via SSH wget on the ESXi host.
func (s *SSHClient) DownloadISO(ctx context.Context, datastore, folder, name, url string) error {
	if strings.Contains(folder, "..") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid folder or name")
	}
	dsPath := "/vmfs/volumes/" + datastore
	if folder != "" {
		dsPath += "/" + strings.TrimPrefix(folder, "/")
	}
	if out, err := s.Run(ctx, "mkdir -p "+shq(dsPath)); err != nil {
		return fmt.Errorf("mkdir: %w (output: %s)", err, out)
	}
	outPath := dsPath + "/" + name
	cmd := "wget --no-check-certificate -q -O " + shq(outPath) + " " + shq(url)
	if out, err := s.Run(ctx, cmd); err != nil {
		return fmt.Errorf("wget: %w (output: %s)", err, out)
	}
	return nil
}

// DownloadISOHTTP downloads a URL via HTTP on the FreeCenter server and uploads
// it to the datastore via SSH (cat pipe). Used when ESXi BusyBox wget cannot
// reach the internet (HTTPS limitation) and govmomi upload is license-gated.
func (c *Client) DownloadISOHTTP(ctx context.Context, datastore, folder, name, url string) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	if strings.Contains(folder, "..") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid folder or name")
	}
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
	tmp, err := os.CreateTemp("", "fc-iso-*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("download write: %w", err)
	}
	tmp.Close()
	// upload via SSH cat pipe (bypasses govmomi license gate)
	dsPath := "/vmfs/volumes/" + datastore
	if folder != "" {
		dsPath += "/" + strings.TrimPrefix(folder, "/")
	}
	remotePath := dsPath + "/" + name
	if err := c.sshPutFile(ctx, remotePath, tmp.Name()); err != nil {
		return fmt.Errorf("ssh upload: %w", err)
	}
	return nil
}

// sshPutFile copies a local file to a remote path via SSH (cat pipe).
func (c *Client) sshPutFile(ctx context.Context, remotePath, localFile string) error {
	f, err := os.Open(localFile)
	if err != nil {
		return err
	}
	defer f.Close()
	// ensure parent dir exists
	dir := path.Dir(remotePath)
	if out, err := c.sshRun(ctx, "mkdir -p "+shq(dir)); err != nil {
		return fmt.Errorf("mkdir %s: %w (output: %s)", dir, err, out)
	}
	return c.sshPut(ctx, "cat > "+shq(remotePath), f)
}
