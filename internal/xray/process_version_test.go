package xray

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestRefreshVersionTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}

	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)
	binaryPath := filepath.Join(dir, GetBinaryName())
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\nexec sleep 1\n"), 0o700); err != nil {
		t.Fatalf("write xray fixture: %v", err)
	}

	previousTimeout := xrayVersionTimeout
	xrayVersionTimeout = 20 * time.Millisecond
	t.Cleanup(func() { xrayVersionTimeout = previousTimeout })

	p := newProcess(&Config{})
	started := time.Now()
	p.refreshVersion()
	elapsed := time.Since(started)
	if elapsed < xrayVersionTimeout {
		t.Fatalf("refreshVersion duration = %s, want at least %s", elapsed, xrayVersionTimeout)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("refreshVersion duration = %s, want under 500ms", elapsed)
	}
	if got := p.GetXrayVersion(); got != "Unknown" {
		t.Fatalf("version = %q, want Unknown", got)
	}
}

func TestStartPassesSharedAssetsToRelocatedCore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	initProcessTestLogger(t)
	root := t.TempDir()
	shared, release := filepath.Join(root, "shared"), filepath.Join(root, "release")
	for _, path := range []string{shared, release} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XUI_BIN_FOLDER", shared)
	t.Setenv("XUI_LOG_FOLDER", root)
	t.Setenv("XRAY_LOCATION_ASSET", "")
	t.Setenv("xray.location.asset", "")
	capture := filepath.Join(root, "assets-path")
	t.Setenv("XUI_TEST_ASSET_CAPTURE", capture)
	core := filepath.Join(release, GetBinaryName())
	fixture := "#!/bin/sh\nif [ \"$1\" = '-version' ]; then echo 'Xray test'; exit 0; fi\nprintf '%s' \"$XRAY_LOCATION_ASSET\" > \"$XUI_TEST_ASSET_CAPTURE\"\nexec sleep 30\n"
	if err := os.WriteFile(core, []byte(fixture), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(core, filepath.Join(shared, GetBinaryName())); err != nil {
		t.Fatal(err)
	}
	p := newProcess(&Config{})
	p.configPath = filepath.Join(shared, "config.json")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Stop() })
	deadline := time.Now().Add(time.Second)
	for {
		body, err := os.ReadFile(capture)
		if err == nil && string(body) == shared {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child asset directory = %q, want %q; read error: %v", body, shared, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestProcessConfigSnapshotsAreRaceSafe(t *testing.T) {
	p := NewProcess(&Config{})
	first := &Config{}
	second := &Config{}
	var wg sync.WaitGroup

	wg.Go(func() {
		for range 1000 {
			p.SetConfig(first)
			p.SetConfig(second)
		}
	})
	for range 4 {
		wg.Go(func() {
			for range 1000 {
				if p.GetConfig() == nil {
					t.Error("config = nil")
				}
			}
		})
	}
	wg.Wait()
}
