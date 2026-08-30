package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danieljhkim/monodev/internal/config"
	"github.com/danieljhkim/monodev/internal/state"
	"github.com/danieljhkim/monodev/internal/stores"
)

// scopedMockStoreRepo extends mockStoreRepo with list and meta support for scope tests
type scopedMockStoreRepo struct {
	storeIDs map[string]bool
	metas    map[string]*stores.StoreMeta
	tracks   map[string]*stores.TrackFile
	created  map[string]*stores.StoreMeta
}

func newScopedMockStoreRepo() *scopedMockStoreRepo {
	return &scopedMockStoreRepo{
		storeIDs: make(map[string]bool),
		metas:    make(map[string]*stores.StoreMeta),
		tracks:   make(map[string]*stores.TrackFile),
		created:  make(map[string]*stores.StoreMeta),
	}
}

func (m *scopedMockStoreRepo) Exists(id string) (bool, error) { return m.storeIDs[id], nil }
func (m *scopedMockStoreRepo) List() ([]string, error) {
	var ids []string
	for id := range m.storeIDs {
		ids = append(ids, id)
	}
	return ids, nil
}
func (m *scopedMockStoreRepo) Create(id string, meta *stores.StoreMeta) error {
	m.storeIDs[id] = true
	m.metas[id] = meta
	m.created[id] = meta
	return nil
}
func (m *scopedMockStoreRepo) LoadMeta(id string) (*stores.StoreMeta, error) {
	if meta, ok := m.metas[id]; ok {
		return meta, nil
	}
	return nil, errors.New("store not found")
}
func (m *scopedMockStoreRepo) SaveMeta(id string, meta *stores.StoreMeta) error {
	m.metas[id] = meta
	return nil
}
func (m *scopedMockStoreRepo) LoadTrack(id string) (*stores.TrackFile, error) {
	if track, ok := m.tracks[id]; ok {
		return track, nil
	}
	return stores.NewTrackFile(), nil
}
func (m *scopedMockStoreRepo) SaveTrack(id string, track *stores.TrackFile) error {
	m.tracks[id] = track
	return nil
}
func (m *scopedMockStoreRepo) OverlayRoot(id string) string { return "/overlay/" + id }
func (m *scopedMockStoreRepo) Delete(id string) error {
	delete(m.storeIDs, id)
	delete(m.metas, id)
	return nil
}

// newScopedTestEngine creates an engine with separate global and component store repos
func newScopedTestEngine(globalRepo, componentRepo *scopedMockStoreRepo) *Engine {
	stateStore := newMockStateStore()
	return newScopedTestEngineWithState(globalRepo, componentRepo, stateStore)
}

func newScopedTestEngineWithState(globalRepo, componentRepo *scopedMockStoreRepo, stateStore *mockStateStore) *Engine {
	paths := config.Paths{
		Root:       "/tmp/monodev",
		Stores:     "/tmp/monodev/stores",
		Workspaces: "/tmp/monodev/workspaces",
	}
	e := New(
		&mockGitRepo{},
		globalRepo,
		stateStore,
		&mockFS{},
		&mockHasher{},
		&mockClock{},
		paths,
	)
	var component stores.StoreRepo
	if componentRepo != nil {
		component = componentRepo
	}
	e.storeResolver = newEngineStoreResolver(globalRepo, globalRepo, component)
	return e
}

func TestCreateStore_GlobalScope(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, componentRepo)

	err := eng.CreateStore(context.Background(), &CreateStoreRequest{
		CWD:     "/repo",
		StoreID: "my-store",
		Name:    "my-store",
		Scope:   stores.ScopeGlobal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify store was created in global repo
	if _, ok := globalRepo.created["my-store"]; !ok {
		t.Error("expected store to be created in global repo")
	}
	// Verify store was NOT created in component repo
	if _, ok := componentRepo.created["my-store"]; ok {
		t.Error("expected store NOT to be created in component repo")
	}
}

func TestCreateStore_ComponentScope(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, componentRepo)

	err := eng.CreateStore(context.Background(), &CreateStoreRequest{
		CWD:     "/repo",
		StoreID: "comp-store",
		Name:    "comp-store",
		Scope:   stores.ScopeComponent,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify store was created in component repo
	if _, ok := componentRepo.created["comp-store"]; !ok {
		t.Error("expected store to be created in component repo")
	}
	// Verify store was NOT created in global repo
	if _, ok := globalRepo.created["comp-store"]; ok {
		t.Error("expected store NOT to be created in global repo")
	}
}

func TestCreateStore_ComponentScope_NoRepoContext(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	// No component repo (nil)
	eng := newScopedTestEngine(globalRepo, nil)

	err := eng.CreateStore(context.Background(), &CreateStoreRequest{
		CWD:     "/repo",
		StoreID: "comp-store",
		Name:    "comp-store",
		Scope:   stores.ScopeComponent,
	})
	if err == nil {
		t.Fatal("expected error when creating component store without repo context")
	}
}

func TestListStores_BothScopes(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["global-store"] = true
	globalRepo.metas["global-store"] = stores.NewStoreMeta("global-store", time.Now())

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["comp-store"] = true
	componentRepo.metas["comp-store"] = stores.NewStoreMeta("comp-store", time.Now())

	eng := newScopedTestEngine(globalRepo, componentRepo)

	result, err := eng.ListStores(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 stores, got %d", len(result))
	}

	// Global stores should come first
	if result[0].Scope != stores.ScopeGlobal {
		t.Errorf("expected first store to be global, got %s", result[0].Scope)
	}
	if result[1].Scope != stores.ScopeComponent {
		t.Errorf("expected second store to be component, got %s", result[1].Scope)
	}
}

func TestListStores_GlobalOnly(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["g1"] = true
	globalRepo.metas["g1"] = stores.NewStoreMeta("g1", time.Now())

	eng := newScopedTestEngine(globalRepo, nil)

	result, err := eng.ListStores(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 store, got %d", len(result))
	}
	if result[0].ID != "g1" {
		t.Errorf("expected store ID 'g1', got %s", result[0].ID)
	}
}

func TestDescribeStore_BothScopes(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["shared"] = true
	globalRepo.metas["shared"] = stores.NewStoreMeta("shared", time.Now())

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["shared"] = true
	componentRepo.metas["shared"] = stores.NewStoreMeta("shared", time.Now())

	eng := newScopedTestEngine(globalRepo, componentRepo)

	result, err := eng.DescribeStore(context.Background(), "shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 results (both scopes), got %d", len(result))
	}
	if result[0].Scope != stores.ScopeGlobal {
		t.Errorf("expected first result to be global, got %s", result[0].Scope)
	}
	if result[1].Scope != stores.ScopeComponent {
		t.Errorf("expected second result to be component, got %s", result[1].Scope)
	}
}

func TestDescribeStore_NotFound(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, componentRepo)

	_, err := eng.DescribeStore(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent store")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteStore_Ambiguous(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["shared"] = true
	globalRepo.metas["shared"] = stores.NewStoreMeta("shared", time.Now())

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["shared"] = true
	componentRepo.metas["shared"] = stores.NewStoreMeta("shared", time.Now())

	stateStore := newMockStateStore()
	eng := newScopedTestEngineWithState(globalRepo, componentRepo, stateStore)

	result, err := eng.DeleteStore(context.Background(), &DeleteStoreRequest{
		StoreID: "shared",
		Force:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Deleted {
		t.Error("expected the preferred (component) store to be deleted")
	}
	if componentRepo.storeIDs["shared"] {
		t.Error("expected component store to be deleted when ID is ambiguous")
	}
	if !globalRepo.storeIDs["shared"] {
		t.Error("expected global store to remain when ID is ambiguous")
	}
}

func TestDeleteStore_WithScope(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["shared"] = true
	globalRepo.metas["shared"] = stores.NewStoreMeta("shared", time.Now())

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["shared"] = true
	componentRepo.metas["shared"] = stores.NewStoreMeta("shared", time.Now())

	stateStore := newMockStateStore()
	eng := newScopedTestEngineWithState(globalRepo, componentRepo, stateStore)

	result, err := eng.DeleteStore(context.Background(), &DeleteStoreRequest{
		StoreID: "shared",
		Scope:   stores.ScopeGlobal,
		Force:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Deleted {
		t.Error("expected store to be deleted")
	}

	// Verify deleted from global only
	exists, _ := globalRepo.Exists("shared")
	if exists {
		t.Error("expected store to be deleted from global repo")
	}
	exists, _ = componentRepo.Exists("shared")
	if !exists {
		t.Error("expected store to still exist in component repo")
	}
}

func TestUseStore_SearchesBothScopes(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["comp-only"] = true
	componentRepo.metas["comp-only"] = stores.NewStoreMeta("comp-only", time.Now())

	stateStore := newMockStateStore()
	eng := newScopedTestEngineWithState(globalRepo, componentRepo, stateStore)

	err := eng.UseStore(context.Background(), &UseStoreRequest{
		CWD:     "/repo",
		StoreID: "comp-only",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultScope_WithRepoContext(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, componentRepo)

	scope := eng.storeResolver.defaultScope()
	if scope != stores.ScopeComponent {
		t.Errorf("expected default scope 'component' with repo context, got %s", scope)
	}
}

func TestDefaultScope_NoRepoContext(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, nil)

	scope := eng.storeResolver.defaultScope()
	if scope != stores.ScopeGlobal {
		t.Errorf("expected default scope 'global' without repo context, got %s", scope)
	}
}

func TestFindStore_BothScopes(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["shared"] = true

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["shared"] = true

	eng := newScopedTestEngine(globalRepo, componentRepo)

	locations, err := eng.storeResolver.findStore("shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(locations) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(locations))
	}
}

func TestFindStore_OnlyGlobal(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["global-only"] = true

	componentRepo := newScopedMockStoreRepo()

	eng := newScopedTestEngine(globalRepo, componentRepo)

	locations, err := eng.storeResolver.findStore("global-only")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locations))
	}
	if locations[0].Scope != stores.ScopeGlobal {
		t.Errorf("expected global scope, got %s", locations[0].Scope)
	}
}

func TestFindStore_NotFound(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	eng := newScopedTestEngine(globalRepo, componentRepo)

	locations, err := eng.storeResolver.findStore("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(locations) != 0 {
		t.Errorf("expected 0 locations, got %d", len(locations))
	}
}

func TestActiveStoreRepo_WithScope(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["my-store"] = true

	componentRepo := newScopedMockStoreRepo()

	eng := newScopedTestEngine(globalRepo, componentRepo)

	ws := &state.WorkspaceState{
		ActiveStore:      "my-store",
		ActiveStoreScope: stores.ScopeGlobal,
	}

	repo, err := eng.storeResolver.activeStoreRepo(ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo != globalRepo {
		t.Error("expected global repo to be returned")
	}
}

func TestActiveStoreRepo_FallsBackToGlobalWhenComponentUnavailable(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["my-store"] = true

	eng := newScopedTestEngine(globalRepo, nil)

	ws := &state.WorkspaceState{
		ActiveStore:      "my-store",
		ActiveStoreScope: stores.ScopeComponent,
	}

	repo, err := eng.storeResolver.activeStoreRepo(ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo != globalRepo {
		t.Error("expected global repo fallback when component scope is unavailable")
	}
}

func TestActiveStoreRepo_LegacyNoScope(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["my-store"] = true

	eng := newScopedTestEngine(globalRepo, componentRepo)

	ws := &state.WorkspaceState{
		ActiveStore:      "my-store",
		ActiveStoreScope: "", // Legacy - no scope recorded
	}

	repo, err := eng.storeResolver.activeStoreRepo(ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should find it in component and prefer component
	if repo != componentRepo {
		t.Error("expected component repo to be preferred for legacy state")
	}
}

func TestResolveStoreRepo_Ambiguous(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["shared"] = true

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["shared"] = true

	eng := newScopedTestEngine(globalRepo, componentRepo)

	repo, scope, err := eng.storeResolver.resolveStoreRepo("shared", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scope != stores.ScopeComponent {
		t.Errorf("expected component scope for ambiguous ID, got %s", scope)
	}
	if repo != componentRepo {
		t.Error("expected component repo for ambiguous ID")
	}
}

func TestResolveStoreRepo_WithExplicitScope(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["shared"] = true

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["shared"] = true

	eng := newScopedTestEngine(globalRepo, componentRepo)

	repo, scope, err := eng.storeResolver.resolveStoreRepo("shared", stores.ScopeComponent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scope != stores.ScopeComponent {
		t.Errorf("expected component scope, got %s", scope)
	}
	if repo != componentRepo {
		t.Error("expected component repo")
	}
}

func TestMultiStoreRepo_RoutesByID(t *testing.T) {
	repo1 := newScopedMockStoreRepo()
	repo1.storeIDs["store-a"] = true
	repo1.metas["store-a"] = stores.NewStoreMeta("store-a", time.Now())

	repo2 := newScopedMockStoreRepo()
	repo2.storeIDs["store-b"] = true
	repo2.metas["store-b"] = stores.NewStoreMeta("store-b", time.Now())

	mapping := map[string]stores.StoreRepo{
		"store-a": repo1,
		"store-b": repo2,
	}
	multi := stores.NewMultiStoreRepo(mapping, repo1)

	// Check store-a routes to repo1
	meta, err := multi.LoadMeta("store-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "store-a" {
		t.Errorf("expected store-a meta, got %s", meta.Name)
	}

	// Check store-b routes to repo2
	meta, err = multi.LoadMeta("store-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Name != "store-b" {
		t.Errorf("expected store-b meta, got %s", meta.Name)
	}
}

func TestStoreResolverMultiRepoForStoresPrefersComponent(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	globalRepo.storeIDs["shared"] = true
	globalMeta := stores.NewStoreMeta("shared", time.Now())
	globalMeta.Description = "global"
	globalRepo.metas["shared"] = globalMeta

	componentRepo := newScopedMockStoreRepo()
	componentRepo.storeIDs["shared"] = true
	componentMeta := stores.NewStoreMeta("shared", time.Now())
	componentMeta.Description = "component"
	componentRepo.metas["shared"] = componentMeta

	resolver := newEngineStoreResolver(globalRepo, globalRepo, componentRepo)
	multiRepo, err := resolver.multiRepoForStores([]string{"shared"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta, err := multiRepo.LoadMeta("shared")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Description != "component" {
		t.Errorf("expected component repo to be preferred, got description %q", meta.Description)
	}
}

func TestCreateStore_SetsActiveStoreScope(t *testing.T) {
	globalRepo := newScopedMockStoreRepo()
	componentRepo := newScopedMockStoreRepo()
	stateStore := newMockStateStore()
	eng := newScopedTestEngineWithState(globalRepo, componentRepo, stateStore)

	err := eng.CreateStore(context.Background(), &CreateStoreRequest{
		CWD:     "/repo",
		StoreID: "new-store",
		Name:    "new-store",
		Scope:   stores.ScopeComponent,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check workspace state has the scope recorded
	// The workspace ID is derived from the mock git repo
	for _, ws := range stateStore.workspaces {
		if ws.ActiveStore == "new-store" {
			if ws.ActiveStoreScope != stores.ScopeComponent {
				t.Errorf("expected ActiveStoreScope to be 'component', got %q", ws.ActiveStoreScope)
			}
			return
		}
	}
	t.Error("workspace state with new-store not found")
}
