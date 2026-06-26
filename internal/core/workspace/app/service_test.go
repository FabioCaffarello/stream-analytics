package app

import (
	"errors"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/workspace/domain"
)

// ---------------------------------------------------------------------------
// In-memory repository for testing
// ---------------------------------------------------------------------------

type memRepo struct {
	ws        *domain.Workspace
	saveErr   error
	loadErr   error
	deleteErr error
}

func (m *memRepo) Load() (*domain.Workspace, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return m.ws, nil
}

func (m *memRepo) Save(ws *domain.Workspace) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.ws = ws
	return nil
}

func (m *memRepo) Delete() error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.ws = nil
	return nil
}

// ---------------------------------------------------------------------------
// LoadWorkspace
// ---------------------------------------------------------------------------

func TestLoadWorkspace_NoSavedState(t *testing.T) {
	svc := NewWorkspaceService(&memRepo{})
	res := svc.LoadWorkspace()
	if !res.IsOk() {
		t.Fatalf("expected Ok, got problem: %v", res.Problem())
	}
	if res.Value() != nil {
		t.Error("expected nil workspace for first run")
	}
}

func TestLoadWorkspace_WithState(t *testing.T) {
	ws := domain.Reconstitute(10, "V6|a", "fp", map[string]string{"k": "v"}, 1000)
	svc := NewWorkspaceService(&memRepo{ws: ws})
	res := svc.LoadWorkspace()
	if !res.IsOk() {
		t.Fatalf("expected Ok, got problem: %v", res.Problem())
	}
	if res.Value() == nil {
		t.Fatal("expected workspace, got nil")
	}
	if res.Value().SchemaVersion() != 10 {
		t.Errorf("expected schema 10, got %d", res.Value().SchemaVersion())
	}
}

func TestLoadWorkspace_RepoError(t *testing.T) {
	svc := NewWorkspaceService(&memRepo{loadErr: errors.New("disk fail")})
	res := svc.LoadWorkspace()
	if res.IsOk() {
		t.Fatal("expected failure on repo error")
	}
}

// ---------------------------------------------------------------------------
// SaveWorkspace
// ---------------------------------------------------------------------------

func TestSaveWorkspace_Valid(t *testing.T) {
	repo := &memRepo{}
	svc := NewWorkspaceService(repo)
	res := svc.SaveWorkspace(10, "V6|a|b", map[string]string{"theme": "dark"}, 0)
	if !res.IsOk() {
		t.Fatalf("expected Ok, got problem: %v", res.Problem())
	}
	if !res.Value().Saved {
		t.Error("expected Saved=true")
	}
	if res.Value().Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
	if res.Value().SavedAtMs <= 0 {
		t.Error("expected positive saved_at_ms in result")
	}
	if repo.ws == nil {
		t.Fatal("expected workspace to be persisted")
	}
}

func TestSaveWorkspace_ValidationFailed(t *testing.T) {
	svc := NewWorkspaceService(&memRepo{})
	res := svc.SaveWorkspace(0, "V6|a", nil, 0)
	if res.IsOk() {
		t.Fatal("expected failure for invalid schema_version")
	}
}

func TestSaveWorkspace_FutureSchema(t *testing.T) {
	svc := NewWorkspaceService(&memRepo{})
	res := svc.SaveWorkspace(999, "V6|a", nil, 0)
	if res.IsOk() {
		t.Fatal("expected failure for future schema_version")
	}
}

func TestSaveWorkspace_Idempotent(t *testing.T) {
	repo := &memRepo{}
	svc := NewWorkspaceService(repo)

	// First save
	res1 := svc.SaveWorkspace(10, "V6|a", map[string]string{"k": "v"}, 0)
	if !res1.IsOk() {
		t.Fatalf("first save failed: %v", res1.Problem())
	}
	fp1 := res1.Value().Fingerprint

	// Second save with identical content
	res2 := svc.SaveWorkspace(10, "V6|a", map[string]string{"k": "v"}, 0)
	if !res2.IsOk() {
		t.Fatalf("second save failed: %v", res2.Problem())
	}
	fp2 := res2.Value().Fingerprint

	if fp1 != fp2 {
		t.Errorf("identical saves should produce same fingerprint: %s vs %s", fp1, fp2)
	}
}

func TestSaveWorkspace_DifferentContent_NewFingerprint(t *testing.T) {
	repo := &memRepo{}
	svc := NewWorkspaceService(repo)

	res1 := svc.SaveWorkspace(10, "V6|a", nil, 0)
	res2 := svc.SaveWorkspace(10, "V6|b", nil, 0)

	if !res1.IsOk() || !res2.IsOk() {
		t.Fatal("saves should succeed")
	}
	if res1.Value().Fingerprint == res2.Value().Fingerprint {
		t.Error("different content should produce different fingerprints")
	}
}

func TestSaveWorkspace_RepoSaveError(t *testing.T) {
	svc := NewWorkspaceService(&memRepo{saveErr: errors.New("write fail")})
	res := svc.SaveWorkspace(10, "V6|a", nil, 0)
	if res.IsOk() {
		t.Fatal("expected failure on repo save error")
	}
}

func TestSaveWorkspace_StampsSaveTime(t *testing.T) {
	repo := &memRepo{}
	svc := NewWorkspaceService(repo)
	res := svc.SaveWorkspace(10, "V6|a", nil, 0)
	if !res.IsOk() {
		t.Fatalf("save failed: %v", res.Problem())
	}
	if repo.ws.SavedAtMs() <= 0 {
		t.Error("expected positive saved_at_ms after save")
	}
}

func TestSaveWorkspace_PreservesClientTimestamp(t *testing.T) {
	repo := &memRepo{}
	svc := NewWorkspaceService(repo)
	res := svc.SaveWorkspace(10, "V6|a", nil, 1709971200000)
	if !res.IsOk() {
		t.Fatalf("save failed: %v", res.Problem())
	}
	if repo.ws.SavedAtMs() != 1709971200000 {
		t.Errorf("expected client timestamp preserved, got %d", repo.ws.SavedAtMs())
	}
}

// ---------------------------------------------------------------------------
// ResetWorkspace
// ---------------------------------------------------------------------------

func TestResetWorkspace_ClearsState(t *testing.T) {
	ws := domain.Reconstitute(10, "V6|a", "fp", nil, 1000)
	repo := &memRepo{ws: ws}
	svc := NewWorkspaceService(repo)

	res := svc.ResetWorkspace()
	if !res.IsOk() {
		t.Fatalf("expected Ok, got problem: %v", res.Problem())
	}
	if repo.ws != nil {
		t.Error("expected nil workspace after reset")
	}
}

func TestResetWorkspace_AlreadyEmpty(t *testing.T) {
	svc := NewWorkspaceService(&memRepo{})
	res := svc.ResetWorkspace()
	if !res.IsOk() {
		t.Fatalf("expected Ok for reset on empty state: %v", res.Problem())
	}
}

func TestResetWorkspace_RepoError(t *testing.T) {
	svc := NewWorkspaceService(&memRepo{deleteErr: errors.New("rm fail")})
	res := svc.ResetWorkspace()
	if res.IsOk() {
		t.Fatal("expected failure on repo delete error")
	}
}

func TestResetWorkspace_ThenLoad_ReturnsNil(t *testing.T) {
	ws := domain.Reconstitute(10, "V6|a", "fp", nil, 1000)
	repo := &memRepo{ws: ws}
	svc := NewWorkspaceService(repo)

	svc.ResetWorkspace()
	res := svc.LoadWorkspace()
	if !res.IsOk() {
		t.Fatalf("expected Ok: %v", res.Problem())
	}
	if res.Value() != nil {
		t.Error("expected nil after reset")
	}
}

func TestResetWorkspace_ThenSave_Works(t *testing.T) {
	ws := domain.Reconstitute(10, "V6|old", "fp", nil, 1000)
	repo := &memRepo{ws: ws}
	svc := NewWorkspaceService(repo)

	svc.ResetWorkspace()
	res := svc.SaveWorkspace(10, "V6|new", nil, 0)
	if !res.IsOk() {
		t.Fatalf("expected Ok: %v", res.Problem())
	}
	if repo.ws == nil || repo.ws.LayoutV6() != "V6|new" {
		t.Error("expected new workspace after reset+save")
	}
}
