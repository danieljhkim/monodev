package sync

import (
	"testing"
)

func TestBuildPushCommitMessage(t *testing.T) {
	_, _, syncer, _, _, _, cleanup := setupSyncerTest(t)
	defer cleanup()

	tests := []struct {
		name          string
		storeIDs      []string
		withWorkspace bool
		expected      string
	}{
		{
			name:          "single store",
			storeIDs:      []string{"store1"},
			withWorkspace: false,
			expected:      "push: store store1",
		},
		{
			name:          "multiple stores",
			storeIDs:      []string{"store1", "store2", "store3"},
			withWorkspace: false,
			expected:      "push: 3 stores",
		},
		{
			name:          "with workspace",
			storeIDs:      []string{"store1"},
			withWorkspace: true,
			expected:      "push: store store1, workspace",
		},
		{
			name:          "multiple stores with workspace",
			storeIDs:      []string{"store1", "store2"},
			withWorkspace: true,
			expected:      "push: 2 stores, workspace",
		},
		{
			name:          "workspace only",
			storeIDs:      []string{},
			withWorkspace: true,
			expected:      "push: workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := syncer.buildPushCommitMessage(tt.storeIDs, tt.withWorkspace)
			if message != tt.expected {
				t.Errorf("buildPushCommitMessage() = %q, want %q", message, tt.expected)
			}
		})
	}
}
