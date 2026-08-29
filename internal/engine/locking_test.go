package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/danieljhkim/monodev/internal/lockfile"
	"github.com/danieljhkim/monodev/internal/stores"
)

type recordingLockRepo struct {
	keys     map[string]string
	acquired []string
}

func (r *recordingLockRepo) StoreLockKey(id string) (string, error) { return r.keys[id], nil }
func (r *recordingLockRepo) LockStore(_ context.Context, id string, _ lockfile.Mode) (*lockfile.Lock, error) {
	r.acquired = append(r.acquired, id)
	return nil, nil
}
func (r *recordingLockRepo) List() ([]string, error)                     { return nil, nil }
func (r *recordingLockRepo) Exists(string) (bool, error)                 { return true, nil }
func (r *recordingLockRepo) Create(string, *stores.StoreMeta) error      { return nil }
func (r *recordingLockRepo) LoadMeta(string) (*stores.StoreMeta, error)  { return nil, nil }
func (r *recordingLockRepo) SaveMeta(string, *stores.StoreMeta) error    { return nil }
func (r *recordingLockRepo) LoadTrack(string) (*stores.TrackFile, error) { return nil, nil }
func (r *recordingLockRepo) SaveTrack(string, *stores.TrackFile) error   { return nil }
func (r *recordingLockRepo) OverlayRoot(string) string                   { return "" }
func (r *recordingLockRepo) Delete(string) error                         { return nil }

func TestLockStoresUsesCanonicalOrder(t *testing.T) {
	repo := &recordingLockRepo{keys: map[string]string{
		"last":   "/z/last.lock",
		"first":  "/a/first.lock",
		"middle": "/m/middle.lock",
	}}
	eng := &Engine{}
	unlock, err := eng.lockStores(context.Background(),
		storeLockRequest{repo: repo, id: "last", mode: lockfile.Shared},
		storeLockRequest{repo: repo, id: "first", mode: lockfile.Shared},
		storeLockRequest{repo: repo, id: "middle", mode: lockfile.Shared},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	want := []string{"first", "middle", "last"}
	if !reflect.DeepEqual(repo.acquired, want) {
		t.Fatalf("lock order = %v, want %v", repo.acquired, want)
	}
}
