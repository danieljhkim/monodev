package state

import "os"

// scopedStore loads workspace state from primary then secondary.
// New IDs are saved on primary, matching Engine.stateStore (the global
// workspace directory). Existing IDs stay in the store that already holds them.
type scopedStore struct {
	primary   StateStore
	secondary StateStore
}

// NewScopedStore returns a StateStore that searches primary then secondary.
// If secondary is nil, primary is returned unchanged.
func NewScopedStore(primary, secondary StateStore) StateStore {
	if secondary == nil {
		return primary
	}
	return &scopedStore{primary: primary, secondary: secondary}
}

func (s *scopedStore) storeFor(id string) (StateStore, error) {
	_, err := s.primary.LoadWorkspace(id)
	if err == nil {
		return s.primary, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	_, err = s.secondary.LoadWorkspace(id)
	if err == nil {
		return s.secondary, nil
	}
	if os.IsNotExist(err) {
		return s.primary, nil
	}
	return nil, err
}

func (s *scopedStore) LoadWorkspace(id string) (*WorkspaceState, error) {
	ws, err := s.primary.LoadWorkspace(id)
	if err == nil {
		return ws, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return s.secondary.LoadWorkspace(id)
}

func (s *scopedStore) SaveWorkspace(id string, ws *WorkspaceState) error {
	store, err := s.storeFor(id)
	if err != nil {
		return err
	}
	return store.SaveWorkspace(id, ws)
}

func (s *scopedStore) DeleteWorkspace(id string) error {
	store, err := s.storeFor(id)
	if err != nil {
		return err
	}
	return store.DeleteWorkspace(id)
}
