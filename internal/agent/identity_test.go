package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentitySaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id := Identity{WorkerID: "wrk-1", Secret: "secret-123"}
	if err := SaveIdentity(dir, id); err != nil {
		t.Fatalf("SaveIdentity: %v", err)
	}
	got, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("LoadIdentity: %v", err)
	}
	if got == nil {
		t.Fatal("LoadIdentity returned nil")
	}
	if *got != id {
		t.Errorf("got %+v, want %+v", *got, id)
	}
}

func TestIdentityFileMode(t *testing.T) {
	dir := t.TempDir()
	if err := SaveIdentity(dir, Identity{WorkerID: "w", Secret: "s"}); err != nil {
		t.Fatalf("SaveIdentity: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestIdentityDirMode(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "data") // does not exist yet
	if err := SaveIdentity(subdir, Identity{WorkerID: "w", Secret: "s"}); err != nil {
		t.Fatalf("SaveIdentity: %v", err)
	}
	info, err := os.Stat(subdir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("dir mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestLoadIdentityMissingReturnsErrNoIdentity(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadIdentity(dir)
	if !errors.Is(err, ErrNoIdentity) {
		t.Errorf("err = %v, want ErrNoIdentity", err)
	}
}

func TestLoadIdentityCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadIdentity(dir)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if errors.Is(err, ErrNoIdentity) {
		t.Errorf("corrupt JSON returned ErrNoIdentity, want a different error")
	}
}

func TestIdentitySaveTwiceOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := SaveIdentity(dir, Identity{WorkerID: "w1", Secret: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(dir, Identity{WorkerID: "w2", Secret: "s2"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.WorkerID != "w2" || got.Secret != "s2" {
		t.Errorf("got %+v, want w2/s2", got)
	}
}

func TestIdentityNoTmpFilesLeft(t *testing.T) {
	dir := t.TempDir()
	if err := SaveIdentity(dir, Identity{WorkerID: "w", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestDeleteIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := SaveIdentity(dir, Identity{WorkerID: "w", Secret: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteIdentity(dir); err != nil {
		t.Fatalf("DeleteIdentity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "identity.json")); !os.IsNotExist(err) {
		t.Errorf("file still exists after DeleteIdentity")
	}
	// safe to call twice
	if err := DeleteIdentity(dir); err != nil {
		t.Errorf("DeleteIdentity second call: %v", err)
	}
}
