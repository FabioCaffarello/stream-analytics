package observability

import "sync"

type StorageStateSnapshot struct {
	Hot       StoragePathState
	Cold      StoragePathState
	Committer StorageCommitterState
}

type StoragePathState struct {
	LastOKKnown     bool
	LastOK          bool
	LastError       string
	FailsTotalKnown bool
	FailsTotal      uint64
}

type StorageCommitterState struct {
	LastOKKnown bool
	LastOK      bool
	LastError   string
}

type storageStateStore struct {
	mu       sync.Mutex
	snapshot StorageStateSnapshot
}

var globalStorageStateStore = newStorageStateStore()

func newStorageStateStore() *storageStateStore {
	return &storageStateStore{}
}

func SetHotOk() {
	globalStorageStateStore.setHotOK()
}

func SetHotErr(err error) {
	globalStorageStateStore.setHotErr(err)
}

func SetColdOk() {
	globalStorageStateStore.setColdOK()
}

func SetColdErr(err error) {
	globalStorageStateStore.setColdErr(err)
}

func SetCommitterOk() {
	globalStorageStateStore.setCommitterOK()
}

func SetCommitterErr(err error) {
	globalStorageStateStore.setCommitterErr(err)
}

func SnapshotStorageState() StorageStateSnapshot {
	return globalStorageStateStore.snapshotState()
}

func (s *storageStateStore) setHotOK() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Hot.LastOKKnown = true
	s.snapshot.Hot.LastOK = true
	s.snapshot.Hot.LastError = ""
}

func (s *storageStateStore) setHotErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Hot.LastOKKnown = true
	s.snapshot.Hot.LastOK = false
	s.snapshot.Hot.LastError = storageErrString(err)
	s.snapshot.Hot.FailsTotalKnown = true
	s.snapshot.Hot.FailsTotal++
}

func (s *storageStateStore) setColdOK() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Cold.LastOKKnown = true
	s.snapshot.Cold.LastOK = true
	s.snapshot.Cold.LastError = ""
}

func (s *storageStateStore) setColdErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Cold.LastOKKnown = true
	s.snapshot.Cold.LastOK = false
	s.snapshot.Cold.LastError = storageErrString(err)
	s.snapshot.Cold.FailsTotalKnown = true
	s.snapshot.Cold.FailsTotal++
}

func (s *storageStateStore) setCommitterOK() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Committer.LastOKKnown = true
	s.snapshot.Committer.LastOK = true
	s.snapshot.Committer.LastError = ""
}

func (s *storageStateStore) setCommitterErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Committer.LastOKKnown = true
	s.snapshot.Committer.LastOK = false
	s.snapshot.Committer.LastError = storageErrString(err)
}

func (s *storageStateStore) snapshotState() StorageStateSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}

func storageErrString(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}
