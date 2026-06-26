package infra

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/workspace/domain"
)

func TestFileStore_LoadEmpty_NilNil(t *testing.T) {
	dir := t.TempDir()
	store := NewFileWorkspaceStore(dir)

	ws, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != nil {
		t.Fatal("expected nil workspace for empty directory")
	}
}

func TestFileStore_SaveThenLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFileWorkspaceStore(dir)

	ws, _ := domain.NewFromPayload(10, "V6|cell1|cell2", map[string]string{"theme": "dark"}, 1709971200000)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected workspace after save")
	}
	if loaded.SchemaVersion() != 10 {
		t.Errorf("schema: expected 10, got %d", loaded.SchemaVersion())
	}
	if loaded.LayoutV6() != "V6|cell1|cell2" {
		t.Errorf("layout: expected V6|cell1|cell2, got %s", loaded.LayoutV6())
	}
	if loaded.Settings()["theme"] != "dark" {
		t.Errorf("settings[theme]: expected dark, got %s", loaded.Settings()["theme"])
	}
}

func TestFileStore_PersistsToFile(t *testing.T) {
	dir := t.TempDir()
	store := NewFileWorkspaceStore(dir)

	ws, _ := domain.NewFromPayload(10, "V6|a", nil, 0)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Verify file exists on disk.
	path := filepath.Join(dir, "workspace.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not found: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("file should not be empty")
	}
}

func TestFileStore_ReopenAndLoad(t *testing.T) {
	dir := t.TempDir()

	// Save with one store instance.
	store1 := NewFileWorkspaceStore(dir)
	ws, _ := domain.NewFromPayload(10, "V6|persist", map[string]string{"k": "v"}, 1000)
	if err := store1.Save(ws); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Open a new store instance and verify it loads from disk.
	store2 := NewFileWorkspaceStore(dir)
	loaded, err := store2.Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected workspace from disk")
	}
	if loaded.LayoutV6() != "V6|persist" {
		t.Errorf("expected V6|persist, got %s", loaded.LayoutV6())
	}
	if loaded.Settings()["k"] != "v" {
		t.Errorf("settings[k]: expected v, got %s", loaded.Settings()["k"])
	}
}

func TestFileStore_CorruptFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")

	// Write corrupt JSON.
	if err := os.WriteFile(path, []byte("{not json}"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	store := NewFileWorkspaceStore(dir)
	ws, err := store.Load()
	if err == nil {
		t.Fatal("expected error for corrupt file")
	}
	if ws != nil {
		t.Fatal("expected nil workspace for corrupt file")
	}
}

func TestFileStore_CorruptFile_SaveClearsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")

	// Write corrupt JSON.
	if err := os.WriteFile(path, []byte("{not json}"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	store := NewFileWorkspaceStore(dir)
	_, err := store.Load()
	if err == nil {
		t.Fatal("expected error for corrupt file")
	}

	// Save valid data — should clear the error.
	ws, _ := domain.NewFromPayload(10, "V6|fixed", nil, 0)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load should succeed after save: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected workspace after save")
	}
}

func TestFileStore_Overwrite(t *testing.T) {
	dir := t.TempDir()
	store := NewFileWorkspaceStore(dir)

	ws1, _ := domain.NewFromPayload(10, "V6|first", nil, 0)
	if err := store.Save(ws1); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	ws2, _ := domain.NewFromPayload(10, "V6|second", nil, 0)
	if err := store.Save(ws2); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	loaded, _ := store.Load()
	if loaded.LayoutV6() != "V6|second" {
		t.Errorf("expected V6|second, got %s", loaded.LayoutV6())
	}
}

func TestFileStore_CreatesMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub", "dir")
	store := NewFileWorkspaceStore(nested)

	ws, _ := domain.NewFromPayload(10, "V6|nested", nil, 0)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save in nested dir failed: %v", err)
	}

	loaded, _ := store.Load()
	if loaded == nil || loaded.LayoutV6() != "V6|nested" {
		t.Error("expected workspace from nested directory")
	}
}

func TestFileStore_AtomicWrite_NoTmpLeftover(t *testing.T) {
	dir := t.TempDir()
	store := NewFileWorkspaceStore(dir)

	ws, _ := domain.NewFromPayload(10, "V6|atomic", nil, 0)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	tmpPath := filepath.Join(dir, "workspace.json.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful save")
	}
}

func TestFileStore_Delete_ClearsState(t *testing.T) {
	dir := t.TempDir()
	store := NewFileWorkspaceStore(dir)

	ws, _ := domain.NewFromPayload(10, "V6|todelete", nil, 0)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load after delete error: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected nil workspace after delete")
	}

	// Verify file is gone from disk.
	path := filepath.Join(dir, "workspace.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("workspace.json should not exist after delete")
	}
}

func TestFileStore_Delete_NoFile_NoError(t *testing.T) {
	dir := t.TempDir()
	store := NewFileWorkspaceStore(dir)

	// Delete when no file exists — should not error.
	if err := store.Delete(); err != nil {
		t.Fatalf("delete on empty dir should not error: %v", err)
	}
}

func TestFileStore_Delete_ThenSave(t *testing.T) {
	dir := t.TempDir()
	store := NewFileWorkspaceStore(dir)

	ws, _ := domain.NewFromPayload(10, "V6|first", nil, 0)
	_ = store.Save(ws)
	_ = store.Delete()

	ws2, _ := domain.NewFromPayload(10, "V6|second", nil, 0)
	if err := store.Save(ws2); err != nil {
		t.Fatalf("save after delete failed: %v", err)
	}

	loaded, _ := store.Load()
	if loaded == nil || loaded.LayoutV6() != "V6|second" {
		t.Error("expected V6|second after delete+save")
	}
}

func TestFileStore_FingerprintPreservedAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	store1 := NewFileWorkspaceStore(dir)

	ws, _ := domain.NewFromPayload(10, "V6|fp", map[string]string{"a": "b"}, 0)
	_ = store1.Save(ws)
	fp1 := ws.Fingerprint()

	store2 := NewFileWorkspaceStore(dir)
	loaded, _ := store2.Load()
	if loaded.Fingerprint() != fp1 {
		t.Errorf("fingerprint mismatch: %s vs %s", fp1, loaded.Fingerprint())
	}
}
