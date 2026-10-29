package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// useVersion sets the running version for the test and restores it afterwards.
func useVersion(t *testing.T, v string) {
	t.Helper()
	prev := Version
	Version = v
	t.Cleanup(func() { Version = prev })
}

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"0.3.1", [3]int{0, 3, 1}, true},
		{"v1.10.2", [3]int{1, 10, 2}, true},
		{"dev", [3]int{}, false},
		{"0.3", [3]int{}, false},
		{"0.3.1-rc1", [3]int{}, false},
		{"", [3]int{}, false},
	} {
		got, ok := parseVersion(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestUpdateStatus(t *testing.T) {
	rel := func(tag string) *githubRelease {
		return &githubRelease{TagName: tag, HTMLURL: "https://example.test/release"}
	}
	for _, tc := range []struct {
		name      string
		current   string
		tag       string
		available bool
	}{
		{"newer patch", "0.3.1", "v0.3.2", true},
		{"newer minor beats higher patch", "0.3.9", "v0.4.0", true},
		{"same version", "0.3.1", "v0.3.1", false},
		{"older release", "0.4.0", "v0.3.9", false},
		{"development build", "dev", "v0.3.1", false},
		{"malformed tag", "0.3.1", "latest", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := updateStatus(tc.current, rel(tc.tag))
			if info.Available != tc.available {
				t.Errorf("Available = %v, want %v", info.Available, tc.available)
			}
			if info.Current != tc.current || info.URL != "https://example.test/release" {
				t.Errorf("unexpected info %+v", info)
			}
		})
	}
}

func TestReleaseAsset(t *testing.T) {
	for _, tc := range []struct {
		goos      string
		installed bool
		want      string
	}{
		{"darwin", false, "forgeai-0.3.2-macos-universal.dmg"},
		{"windows", true, "forgeai-0.3.2-windows-amd64-setup.exe"},
		{"windows", false, "forgeai-0.3.2-windows-amd64-portable.exe"},
		{"linux", false, "forgeai-0.3.2-linux-amd64.tar.gz"},
	} {
		got, err := releaseAsset("0.3.2", tc.goos, tc.installed)
		if err != nil || got != tc.want {
			t.Errorf("releaseAsset(%s, installed=%v) = %q, %v; want %q", tc.goos, tc.installed, got, err, tc.want)
		}
	}
	if _, err := releaseAsset("0.3.2", "plan9", false); err == nil {
		t.Error("releaseAsset for an unsupported platform returned no error")
	}
}

func TestParseChecksums(t *testing.T) {
	const sum = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	text := sum + "  forgeai-0.3.2-linux-amd64.tar.gz\r\n" +
		sum + " *forgeai-0.3.2-macos-universal.dmg\n" +
		"not a checksum line\n"
	sums := parseChecksums(text)
	if len(sums) != 2 {
		t.Fatalf("parsed %d entries, want 2: %v", len(sums), sums)
	}
	if sums["forgeai-0.3.2-linux-amd64.tar.gz"] != sum || sums["forgeai-0.3.2-macos-universal.dmg"] != sum {
		t.Errorf("unexpected checksums: %v", sums)
	}
}

func TestVerifyFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "forgeai-0.3.2-linux-amd64.tar.gz")
	content := []byte("release bytes")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if err := verifyFile(p, hex.EncodeToString(sum[:])); err != nil {
		t.Errorf("verifyFile with the right checksum: %v", err)
	}
	if err := verifyFile(p, strings.Repeat("0", 64)); err == nil {
		t.Error("verifyFile accepted a wrong checksum")
	}
}

func TestAppBundle(t *testing.T) {
	for _, tc := range []struct {
		exe    string
		bundle string
		ok     bool
	}{
		{"/Applications/forgeai.app/Contents/MacOS/forgeai", "/Applications/forgeai.app", true},
		{"/Users/me/Apps/forgeai.app/Contents/MacOS/forgeai", "/Users/me/Apps/forgeai.app", true},
		{"/usr/local/bin/forgeai", "", false},
	} {
		got, ok := appBundle(tc.exe)
		if ok != tc.ok || got != tc.bundle {
			t.Errorf("appBundle(%q) = %q, %v; want %q, %v", tc.exe, got, ok, tc.bundle, tc.ok)
		}
	}
}

func TestPsQuote(t *testing.T) {
	if got := psQuote("C:\\it's here"); got != "'C:\\it''s here'" {
		t.Errorf("psQuote = %s", got)
	}
}

// releaseServer serves a fake GitHub release whose assets are files in the
// test server, and points releaseAPI at it.
func releaseServer(t *testing.T, tag string, files map[string][]byte) {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	rel := githubRelease{TagName: tag, HTMLURL: "https://example.test/" + tag}
	for name, data := range files {
		mux.HandleFunc("/files/"+name, func(w http.ResponseWriter, _ *http.Request) { w.Write(data) })
		rel.Assets = append(rel.Assets, githubAsset{Name: name})
	}
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			http.Error(w, "wrong accept header", http.StatusBadRequest)
			return
		}
		for i := range rel.Assets {
			rel.Assets[i].URL = srv.URL + "/files/" + rel.Assets[i].Name
		}
		json.NewEncoder(w).Encode(rel)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	prev := releaseAPI
	releaseAPI = srv.URL + "/release"
	t.Cleanup(func() { releaseAPI = prev })
}

func TestFetchLatestRelease(t *testing.T) {
	releaseServer(t, "v0.3.2", map[string][]byte{"forgeai-0.3.2-linux-amd64.tar.gz": []byte("x")})
	rel, err := fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.version() != "0.3.2" {
		t.Errorf("version = %q, want 0.3.2", rel.version())
	}
	if _, ok := rel.assetURL("forgeai-0.3.2-linux-amd64.tar.gz"); !ok {
		t.Error("asset missing from the release")
	}
}

func TestCheckUpdate(t *testing.T) {
	releaseServer(t, "v0.3.2", nil)
	useVersion(t, "0.3.1")
	a := &App{ctx: context.Background()}
	info, err := a.CheckUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != "0.3.2" || info.Current != "0.3.1" {
		t.Errorf("CheckUpdate = %+v", info)
	}
}

func TestCheckUpdateDevBuildSkipsNetwork(t *testing.T) {
	// releaseAPI points nowhere: a development build must not ask for it.
	prev := releaseAPI
	releaseAPI = "http://127.0.0.1:1/unreachable"
	t.Cleanup(func() { releaseAPI = prev })
	useVersion(t, devVersion)
	info, err := (&App{ctx: context.Background()}).CheckUpdate()
	if err != nil || info.Available || info.Current != devVersion {
		t.Errorf("CheckUpdate on a dev build = %+v, %v", info, err)
	}
}

// tarballWith returns a gzipped tarball holding one file named name.
func tarballWith(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallReleaseLinux(t *testing.T) {
	const version = "0.3.2"
	asset := "forgeai-" + version + "-linux-amd64.tar.gz"
	tarball := tarballWith(t, "forgeai", []byte("new build"))
	sum := sha256.Sum256(tarball)
	sums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)
	releaseServer(t, "v"+version, map[string][]byte{
		asset:            tarball,
		"SHA256SUMS.txt": []byte(sums),
	})
	rel, err := fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(t.TempDir(), "forgeai")
	if err := os.WriteFile(exe, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	var steps []string
	var started []*exec.Cmd
	start := func(cmd *exec.Cmd) error {
		started = append(started, cmd)
		return nil
	}
	plan := updatePlan{goos: "linux", exe: exe}
	err = installRelease(context.Background(), rel, plan, func(s string) { steps = append(steps, s) }, start)
	if err != nil {
		t.Fatalf("installRelease: %v", err)
	}

	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new build" {
		t.Errorf("executable = %q, %v; want the new build", got, err)
	}
	info, err := os.Stat(exe)
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("executable is not runnable: %v, %v", info, err)
	}
	if want := []string{"download", "verify", "install"}; fmt.Sprint(steps) != fmt.Sprint(want) {
		t.Errorf("steps = %v, want %v", steps, want)
	}
	if len(started) != 1 || !strings.Contains(strings.Join(started[0].Args, " "), exe) {
		t.Fatalf("start was not given a command for %s: %v", exe, started)
	}
}

func TestInstallReleaseRejectsBadChecksum(t *testing.T) {
	const version = "0.3.2"
	asset := "forgeai-" + version + "-linux-amd64.tar.gz"
	tarball := tarballWith(t, "forgeai", []byte("tampered build"))
	sums := fmt.Sprintf("%s  %s\n", strings.Repeat("a", 64), asset)
	releaseServer(t, "v"+version, map[string][]byte{
		asset:            tarball,
		"SHA256SUMS.txt": []byte(sums),
	})
	rel, err := fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "forgeai")
	if err := os.WriteFile(exe, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := func(*exec.Cmd) error { t.Fatal("started a build that failed its checksum"); return nil }
	err = installRelease(context.Background(), rel, updatePlan{goos: "linux", exe: exe}, func(string) {}, start)
	if err == nil {
		t.Fatal("installRelease accepted a file that does not match its checksum")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old build" {
		t.Errorf("executable changed to %q after a failed verification", got)
	}
}

// TestAfterExitRunsOnceProcessEnds checks that the helper waits for the process
// it is given before it runs its script, with the arguments in order.
func TestAfterExitRunsOnceProcessEnds(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the helper is a POSIX shell script")
	}
	waiter := exec.Command("sleep", "0.3")
	if err := waiter.Start(); err != nil {
		t.Fatal(err)
	}
	// Reap the process as soon as it ends: a zombie still answers kill -0.
	reaped := make(chan error, 1)
	go func() { reaped <- waiter.Wait() }()
	out := filepath.Join(t.TempDir(), "out")
	start := time.Now()
	if err := afterExit(waiter.Process.Pid, `printf %s "$2" > "$3"`, "done", out).Run(); err != nil {
		t.Fatalf("helper: %v", err)
	}
	<-reaped
	if time.Since(start) < 200*time.Millisecond {
		t.Errorf("helper ran before the process exited")
	}
	if got, _ := os.ReadFile(out); string(got) != "done" {
		t.Errorf("helper wrote %q, want %q", got, "done")
	}
}

// TestInstallReleaseMacOS replaces an app bundle with the one on a real disk
// image built for the test. It runs only on macOS, where hdiutil and ditto exist.
func TestInstallReleaseMacOS(t *testing.T) {
	if goruntime.GOOS != "darwin" {
		t.Skip("needs hdiutil and ditto")
	}
	const version = "0.3.2"
	asset := "forgeai-" + version + "-macos-universal.dmg"

	// The release disk image holds the new app bundle.
	stage := t.TempDir()
	newExe := filepath.Join(stage, macAppName, "Contents", "MacOS", "forgeai")
	if err := os.MkdirAll(filepath.Dir(newExe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newExe, []byte("new build"), 0o755); err != nil {
		t.Fatal(err)
	}
	dmg := filepath.Join(t.TempDir(), asset)
	if out, err := exec.Command("hdiutil", "create", "-quiet", "-volname", "forgeai", "-srcfolder", stage,
		"-ov", "-format", "UDZO", dmg).CombinedOutput(); err != nil {
		t.Fatalf("hdiutil create: %v: %s", err, out)
	}
	image, err := os.ReadFile(dmg)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	releaseServer(t, "v"+version, map[string][]byte{
		asset:            image,
		"SHA256SUMS.txt": []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), asset)),
	})
	rel, err := fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// The running app is an old bundle in a directory of its own.
	installed := filepath.Join(t.TempDir(), macAppName)
	oldExe := filepath.Join(installed, "Contents", "MacOS", "forgeai")
	if err := os.MkdirAll(filepath.Dir(oldExe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldExe, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}

	var started *exec.Cmd
	start := func(cmd *exec.Cmd) error {
		started = cmd
		return nil
	}
	plan := updatePlan{goos: "darwin", exe: oldExe}
	if err := installRelease(context.Background(), rel, plan, func(string) {}, start); err != nil {
		t.Fatalf("installRelease: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(installed, "Contents", "MacOS", "forgeai"))
	if err != nil || string(got) != "new build" {
		t.Errorf("bundle executable = %q, %v; want the new build", got, err)
	}
	if _, err := os.Stat(installed + ".new"); !os.IsNotExist(err) {
		t.Errorf("staged bundle was left behind: %v", err)
	}
	if started == nil || !strings.Contains(strings.Join(started.Args, " "), installed) {
		t.Fatalf("start was not given a command that reopens %s: %v", installed, started)
	}
}
