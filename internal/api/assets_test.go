package api

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAsset(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAssetVersionChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	writeAsset(t, dir, "css/tailwind.css", "body{}")
	writeAsset(t, dir, "js/app.js", "var a")

	v1 := AssetVersion(dir)
	if len(v1) != 12 || v1 != AssetVersion(dir) {
		t.Fatalf("version %q should be a stable 12-char hash", v1)
	}

	writeAsset(t, dir, "css/tailwind.css", "body{color:red}")
	if v2 := AssetVersion(dir); v2 == v1 {
		t.Fatal("version should change when the CSS changes")
	}
}

func TestAssetVersionMissingFiles(t *testing.T) {
	if got := AssetVersion(t.TempDir()); got != "dev" {
		t.Fatalf("AssetVersion = %q, want dev", got)
	}
}
