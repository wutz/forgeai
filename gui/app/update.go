package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Self-update: the GUI compares its build with the latest GitHub release of
// this repository and, when that is newer, installs the release file for the
// platform in place of the running build. The files are the ones the release
// workflow (.github/workflows/gui-release.yml) publishes, checked against its
// SHA256SUMS.txt.

// Version is the GUI release this binary was built from. The release workflow
// sets it with -ldflags; a local build keeps devVersion, which is never offered
// an update.
var Version = devVersion

const (
	devVersion = "dev"
	// updateRepo is the GitHub repository whose latest release is the GUI release.
	updateRepo = "wutz/forgeai"
	// updateProgressEvent carries the install step to the UI: download, verify,
	// install or restart.
	updateProgressEvent = "update:progress"
	// checksumsName is the release file that lists the SHA-256 of every installer.
	checksumsName = "SHA256SUMS.txt"
	// binaryName is the executable inside the Linux release tarball.
	binaryName = "forgeai"
	// macAppName is the bundle inside the macOS disk image.
	macAppName = "forgeai.app"

	updateCheckTimeout   = 15 * time.Second
	updateInstallTimeout = 15 * time.Minute
	// maxReleaseBytes bounds the release metadata and checksum files read from GitHub.
	maxReleaseBytes = 1 << 20
)

// releaseAPI is the endpoint of the newest published release. Tests replace it.
var releaseAPI = "https://api.github.com/repos/" + updateRepo + "/releases/latest"

// updateClient fetches releases and their files. Each request is bounded by its context.
var updateClient = &http.Client{}

// UpdateInfo is the outcome of an update check.
type UpdateInfo struct {
	// Current is the version of the running build.
	Current string `json:"current"`
	// Latest is the version of the newest release.
	Latest string `json:"latest"`
	// Available is set when Latest is newer than Current.
	Available bool `json:"available"`
	// URL is the page of the newest release, for a manual install.
	URL string `json:"url"`
}

// githubRelease is the part of a GitHub release the update reads.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset is a file attached to a GitHub release.
type githubAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// version is the release version: its tag without the leading v.
func (r *githubRelease) version() string { return strings.TrimPrefix(r.TagName, "v") }

// assetURL returns the download URL of the named file of the release.
func (r *githubRelease) assetURL(name string) (string, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.URL, true
		}
	}
	return "", false
}

// CheckUpdate reports whether a GUI release newer than this build is published.
// A development build never reports one.
func (a *App) CheckUpdate() (UpdateInfo, error) {
	if Version == devVersion {
		return UpdateInfo{Current: Version}, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, updateCheckTimeout)
	defer cancel()
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return UpdateInfo{Current: Version}, err
	}
	return updateStatus(Version, rel), nil
}

// InstallUpdate installs the newest GUI release in place of this build and
// starts it again. The app quits once the new version is in place; the call
// returns just before that.
func (a *App) InstallUpdate() error {
	if Version == devVersion {
		return errors.New("this is a development build, which does not update itself")
	}
	exe, err := runningExecutable()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, updateInstallTimeout)
	defer cancel()
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	if !updateStatus(Version, rel).Available {
		return fmt.Errorf("forgeai %s is already the latest version", Version)
	}
	plan := updatePlan{
		goos:      goruntime.GOOS,
		exe:       exe,
		installed: goruntime.GOOS == "windows" && installedOnWindows(exe),
	}
	progress := func(step string) { a.emitEvent(a.ctx, updateProgressEvent, step) }
	if err := installRelease(ctx, rel, plan, progress, (*exec.Cmd).Start); err != nil {
		return err
	}
	progress("restart")
	wailsruntime.Quit(a.ctx)
	return nil
}

// updatePlan is what an install needs to know about the running build.
type updatePlan struct {
	goos string
	// exe is the running executable, with symlinks resolved.
	exe string
	// installed is set for a Windows build installed by the NSIS installer,
	// rather than the portable executable.
	installed bool
}

// installRelease installs rel in place of the running build described by plan.
// It downloads the release file for the platform, checks it against the
// release checksums and swaps it in. The command that starts the new version
// is passed to start, which runs once this process has exited; the caller
// quits the app after installRelease returns.
func installRelease(ctx context.Context, rel *githubRelease, plan updatePlan, progress func(step string), start func(*exec.Cmd) error) error {
	asset, err := releaseAsset(rel.version(), plan.goos, plan.installed)
	if err != nil {
		return err
	}
	assetURL, ok := rel.assetURL(asset)
	if !ok {
		return fmt.Errorf("release %s has no %s", rel.TagName, asset)
	}
	sumsURL, ok := rel.assetURL(checksumsName)
	if !ok {
		return fmt.Errorf("release %s has no %s", rel.TagName, checksumsName)
	}

	dir, err := os.MkdirTemp("", "forgeai-update-")
	if err != nil {
		return err
	}
	// The Windows installer runs after this process exits, so its directory
	// is removed by the command that runs it.
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(dir)
		}
	}()

	progress("download")
	sums, err := fetchText(ctx, sumsURL)
	if err != nil {
		return err
	}
	want, ok := parseChecksums(sums)[asset]
	if !ok {
		return fmt.Errorf("%s has no checksum for %s", checksumsName, asset)
	}
	file := filepath.Join(dir, asset)
	if err := download(ctx, assetURL, file); err != nil {
		return err
	}

	progress("verify")
	if err := verifyFile(file, want); err != nil {
		return err
	}

	progress("install")
	pid := os.Getpid()
	var relaunch *exec.Cmd
	switch plan.goos {
	case "darwin":
		relaunch, err = installMacApp(plan.exe, file, filepath.Join(dir, "mount"), pid)
	case "linux":
		relaunch, err = installLinuxBinary(plan.exe, file, pid)
	case "windows":
		relaunch, err = installWindows(plan.exe, file, plan.installed, dir, pid)
		keep = plan.installed
	default:
		err = fmt.Errorf("self-update is not available on %s", plan.goos)
	}
	if err != nil {
		return err
	}
	return start(relaunch)
}

// releaseAsset names the release file the platform installs. The names are the
// ones the release workflow publishes (see gui/README.md, Releases).
func releaseAsset(version, goos string, installed bool) (string, error) {
	name := "forgeai-" + version
	switch goos {
	case "darwin":
		return name + "-macos-universal.dmg", nil
	case "windows":
		if installed {
			return name + "-windows-amd64-setup.exe", nil
		}
		return name + "-windows-amd64-portable.exe", nil
	case "linux":
		return name + "-linux-amd64.tar.gz", nil
	}
	return "", fmt.Errorf("self-update is not available on %s", goos)
}

// installMacApp replaces the app bundle that contains exe with the one on the
// disk image dmg. mount is where the image is attached while it is copied.
func installMacApp(exe, dmg, mount string, pid int) (*exec.Cmd, error) {
	bundle, ok := appBundle(exe)
	if !ok {
		return nil, errors.New("forgeai is not running from an app bundle, so it cannot replace itself")
	}
	if err := os.MkdirAll(mount, 0o755); err != nil {
		return nil, err
	}
	if out, err := exec.Command("hdiutil", "attach", "-quiet", "-nobrowse", "-noautoopen", "-readonly",
		"-mountpoint", mount, dmg).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mount %s: %w: %s", filepath.Base(dmg), err, bytes.TrimSpace(out))
	}
	defer exec.Command("hdiutil", "detach", "-quiet", mount).Run()

	// The copy is made beside the bundle, so the swap is two renames on one volume.
	staged := bundle + ".new"
	old := bundle + ".old"
	os.RemoveAll(staged)
	if out, err := exec.Command("ditto", filepath.Join(mount, macAppName), staged).CombinedOutput(); err != nil {
		os.RemoveAll(staged)
		return nil, fmt.Errorf("copy %s: %w: %s", macAppName, err, bytes.TrimSpace(out))
	}
	if err := os.Rename(bundle, old); err != nil {
		os.RemoveAll(staged)
		return nil, err
	}
	if err := os.Rename(staged, bundle); err != nil {
		os.Rename(old, bundle)
		os.RemoveAll(staged)
		return nil, err
	}
	return afterExit(pid, `open "$2"; rm -rf "$3"`, bundle, old), nil
}

// installLinuxBinary replaces the executable exe with the one in the release
// tarball file. A package-managed install under /usr needs root, so the
// replacement fails there and the error is shown to the user.
func installLinuxBinary(exe, tarball string, pid int) (*exec.Cmd, error) {
	staged := exe + ".new"
	if err := extractBinary(tarball, staged); err != nil {
		os.Remove(staged)
		return nil, err
	}
	if err := os.Rename(staged, exe); err != nil {
		os.Remove(staged)
		return nil, fmt.Errorf("replace %s: %w", exe, err)
	}
	return afterExit(pid, `exec "$2"`, exe), nil
}

// installWindows installs the release file on Windows. An installed build runs
// the NSIS installer silently and starts the program again; a portable build
// replaces its executable.
func installWindows(exe, file string, installed bool, dir string, pid int) (*exec.Cmd, error) {
	if installed {
		return powershell(fmt.Sprintf(
			"Wait-Process -Id %d -ErrorAction SilentlyContinue; "+
				"Start-Process -Wait -FilePath %s -ArgumentList '/S'; "+
				"Remove-Item -Recurse -Force %s; "+
				"Start-Process -FilePath %s",
			pid, psQuote(file), psQuote(dir), psQuote(exe))), nil
	}
	// A running executable can be renamed on Windows, so the old one moves
	// aside and is removed once this process has exited.
	staged := exe + ".new"
	old := exe + ".old"
	if err := copyFile(file, staged, 0o755); err != nil {
		return nil, err
	}
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(staged)
		return nil, fmt.Errorf("replace %s: %w", exe, err)
	}
	if err := os.Rename(staged, exe); err != nil {
		os.Rename(old, exe)
		os.Remove(staged)
		return nil, fmt.Errorf("replace %s: %w", exe, err)
	}
	return powershell(fmt.Sprintf(
		"Wait-Process -Id %d -ErrorAction SilentlyContinue; "+
			"Remove-Item -Force %s -ErrorAction SilentlyContinue; "+
			"Start-Process -FilePath %s",
		pid, psQuote(old), psQuote(exe))), nil
}

// afterExit returns a command that waits for process pid to exit, then runs
// script under /bin/sh. args are the positional parameters $2, $3, ...
func afterExit(pid int, script string, args ...string) *exec.Cmd {
	const wait = `while kill -0 "$1" 2>/dev/null; do sleep 0.2; done; `
	argv := append([]string{"-c", wait + script, "sh", strconv.Itoa(pid)}, args...)
	return exec.Command("/bin/sh", argv...)
}

// powershell returns a hidden Windows PowerShell running script. The script is
// passed base64 encoded, so none of its quoting reaches the command line.
func powershell(script string) *exec.Cmd {
	units := utf16.Encode([]rune(script))
	raw := make([]byte, 0, 2*len(units))
	for _, u := range units {
		raw = append(raw, byte(u), byte(u>>8))
	}
	return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
}

// psQuote quotes s as a PowerShell string literal.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// appBundle returns the .app bundle that contains the executable exe.
func appBundle(exe string) (string, bool) {
	dir := filepath.Dir(exe)
	for {
		if strings.HasSuffix(dir, ".app") {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// installedOnWindows reports whether exe belongs to an install made by the NSIS
// installer, which writes uninstall.exe beside the program.
func installedOnWindows(exe string) bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(exe), "uninstall.exe"))
	return err == nil
}

// runningExecutable returns the path of the running executable.
func runningExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// updateStatus compares the release rel with the running version. A build
// whose version is not a release number, such as a development build, or a
// release whose tag is not one, is never reported as out of date.
func updateStatus(current string, rel *githubRelease) UpdateInfo {
	info := UpdateInfo{Current: current, Latest: rel.version(), URL: rel.HTMLURL}
	cur, okCur := parseVersion(current)
	latest, okLatest := parseVersion(info.Latest)
	if okCur && okLatest {
		info.Available = newerVersion(latest, cur)
	}
	return info
}

var versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// parseVersion reads MAJOR.MINOR.PATCH, with an optional leading v.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	m := versionPattern.FindStringSubmatch(v)
	if m == nil {
		return out, false
	}
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newerVersion reports whether a is a later version than b.
func newerVersion(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// checksumLine matches one line of sha256sum output: the hash, then the file
// name, which may be marked with * for binary mode.
var checksumLine = regexp.MustCompile(`^([0-9a-fA-F]{64}) +\*?(\S+)$`)

// parseChecksums maps each file name in a SHA256SUMS.txt to its SHA-256 in lower case.
func parseChecksums(text string) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if m := checksumLine.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			sums[m[2]] = strings.ToLower(m[1])
		}
	}
	return sums
}

// verifyFile checks that the file at p has the SHA-256 want.
func verifyFile(p, want string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("%s does not match its checksum in %s", filepath.Base(p), checksumsName)
	}
	return nil
}

// extractBinary writes the executable of the gzipped tarball to dst, with mode 0755.
func extractBinary(tarball, dst string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s has no %s executable", filepath.Base(tarball), binaryName)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && path.Base(hdr.Name) == binaryName {
			return writeFile(dst, tr, 0o755)
		}
	}
}

// copyFile copies src to dst with mode.
func copyFile(src, dst string, mode fs.FileMode) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeFile(dst, f, mode)
}

// writeFile creates or truncates dst with the contents of r and mode.
func writeFile(dst string, r io.Reader, mode fs.FileMode) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// The umask can clear bits of mode; the executable bit must stay set.
	return os.Chmod(dst, mode)
}

// fetchLatestRelease reads the newest published release of the repository.
func fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	body, err := fetch(ctx, releaseAPI, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var rel githubRelease
	if err := json.NewDecoder(io.LimitReader(body, maxReleaseBytes)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("read the latest release: %w", err)
	}
	return &rel, nil
}

// fetchText reads a small file of a release.
func fetchText(ctx context.Context, url string) (string, error) {
	body, err := fetch(ctx, url, "")
	if err != nil {
		return "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxReleaseBytes))
	return string(data), err
}

// download writes the file at url to dst.
func download(ctx context.Context, url, dst string) error {
	body, err := fetch(ctx, url, "application/octet-stream")
	if err != nil {
		return err
	}
	defer body.Close()
	return writeFile(dst, body, 0o644)
}

// fetch starts a GET of url and returns the response body, which the caller closes.
func fetch(ctx context.Context, url, accept string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "forgeai-gui/"+Version)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}
