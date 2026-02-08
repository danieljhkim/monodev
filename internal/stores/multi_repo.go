package stores

import "fmt"

// MultiStoreRepo wraps multiple StoreRepo instances and routes operations
// by store ID. This is used when a stack contains stores from both scopes.
type MultiStoreRepo struct {
	// mapping maps store IDs to their StoreRepo
	mapping map[string]StoreRepo

	// fallback is the default StoreRepo for unknown IDs
	fallback StoreRepo
}

// NewMultiStoreRepo creates a MultiStoreRepo from a mapping of store IDs to repos.
func NewMultiStoreRepo(mapping map[string]StoreRepo, fallback StoreRepo) *MultiStoreRepo {
	return &MultiStoreRepo{
		mapping:  mapping,
		fallback: fallback,
	}
}

func (m *MultiStoreRepo) repoFor(id string) StoreRepo {
	if repo, ok := m.mapping[id]; ok {
		return repo
	}
	return m.fallback
}

func (m *MultiStoreRepo) List() ([]string, error) {
	seen := make(map[string]bool)
	var result []string
	for _, repo := range m.mapping {
		ids, err := repo.List()
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	if m.fallback != nil {
		ids, err := m.fallback.List()
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	return result, nil
}

func (m *MultiStoreRepo) Exists(id string) (bool, error) {
	return m.repoFor(id).Exists(id)
}

func (m *MultiStoreRepo) Create(id string, meta *StoreMeta) error {
	return m.repoFor(id).Create(id, meta)
}

func (m *MultiStoreRepo) LoadMeta(id string) (*StoreMeta, error) {
	return m.repoFor(id).LoadMeta(id)
}

func (m *MultiStoreRepo) SaveMeta(id string, meta *StoreMeta) error {
	return m.repoFor(id).SaveMeta(id, meta)
}

func (m *MultiStoreRepo) LoadTrack(id string) (*TrackFile, error) {
	return m.repoFor(id).LoadTrack(id)
}

func (m *MultiStoreRepo) SaveTrack(id string, track *TrackFile) error {
	return m.repoFor(id).SaveTrack(id, track)
}

func (m *MultiStoreRepo) OverlayRoot(id string) string {
	return m.repoFor(id).OverlayRoot(id)
}

func (m *MultiStoreRepo) Delete(id string) error {
	repo := m.repoFor(id)
	if repo == nil {
		return fmt.Errorf("no repo found for store %s", id)
	}
	return repo.Delete(id)
}
