## Downloads

| Platform | File |
|---|---|
| macOS (Apple silicon and Intel) | `forgeai-@VERSION@-macos-universal.dmg`: open it and drag forgeai into Applications |
| Windows (x64) | `forgeai-@VERSION@-windows-amd64-setup.exe`: installer. `forgeai-@VERSION@-windows-amd64-portable.exe` runs without installing |
| Linux (x64) | `forgeai-@VERSION@-linux-amd64.deb` (`sudo apt install ./forgeai-@VERSION@-linux-amd64.deb`), or `forgeai-@VERSION@-linux-amd64.tar.gz` for the bare binary |

The builds are not code-signed or notarized yet.

- **macOS:** on the first launch, Gatekeeper blocks forgeai. Open System Settings → Privacy & Security and choose **Open Anyway**, or run `xattr -dr com.apple.quarantine /Applications/forgeai.app`.
- **Windows:** SmartScreen may warn. Choose **More info → Run anyway**.
- **Linux:** the app needs GTK 3 and WebKitGTK 4.1 at runtime; the `.deb` installs them.

forgeai needs a kubeconfig (`$KUBECONFIG` or `~/.kube/config`) for a cluster that runs the forgeai operator. Check the downloads against `SHA256SUMS.txt`.
