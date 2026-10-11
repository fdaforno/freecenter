# Screenshots and demo

`capture.mjs` regenerates everything in `docs/media` from a running
Hostwarden instance: the five screenshots used in the main README and the
demo video (`hostwarden-demo.mp4` and `.gif`).

It only reads and navigates. The New Virtual Machine wizard is walked to the
cloud-init page and cancelled, so nothing is created on the hosts.

## Requirements

- Node.js 20 or newer, and `ffmpeg` for the video (`brew install ffmpeg`).
- A host with at least one powered-on virtual machine, and a cloud image on
  its first datastore (download one from the wizard's catalog once).

## Run

```bash
cd tools/screenshots
npm install
npx playwright install chromium

HW_URL=http://192.168.251.11:8080 HW_USER=admin HW_PASS='...' npm run capture
```

| Variable | Default | |
|---|---|---|
| `HW_URL` | (required) | address of the Hostwarden console |
| `HW_USER` / `HW_PASS` | `admin` / (required) | account used to sign in |
| `HW_HOST` | first connected host | name of the host to show |
| `HW_VM` | first powered-on VM | name of the VM whose console is shown |
| `HW_ONLY` | both | `shots` or `demo` to run one part only |

Check the result in `docs/media` and commit it.
