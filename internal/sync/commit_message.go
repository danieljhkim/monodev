package sync

import (
	"fmt"
	"strings"
)

// buildPushCommitMessage builds a commit message for a push operation.
func (s *Syncer) buildPushCommitMessage(storeIDs []string, withWorkspace bool) string {
	var parts []string

	if len(storeIDs) > 0 {
		if len(storeIDs) == 1 {
			parts = append(parts, fmt.Sprintf("store %s", storeIDs[0]))
		} else {
			parts = append(parts, fmt.Sprintf("%d stores", len(storeIDs)))
		}
	}

	if withWorkspace {
		parts = append(parts, "workspace")
	}

	return fmt.Sprintf("push: %s", strings.Join(parts, ", "))
}
