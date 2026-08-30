package state

import (
	"encoding/json"
	"fmt"
)

// WorkspaceSchemaVersion is the newest workspace-state format this binary can
// read and write.
const WorkspaceSchemaVersion = 2

// SchemaVersion reads only a persisted file's schema header. Callers must
// check this before decoding the full format so a newer file is never
// partially interpreted by an older binary.
func SchemaVersion(filePath string, data []byte) (int, error) {
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return 0, fmt.Errorf("cannot read schemaVersion from %s: %w", filePath, err)
	}
	return header.SchemaVersion, nil
}

// CheckSchemaVersion rejects only schemas from a newer binary. Older schemas
// remain readable so their owning format can migrate them deliberately.
func CheckSchemaVersion(filePath string, data []byte, supported int) (int, error) {
	found, err := SchemaVersion(filePath, data)
	if err != nil {
		return 0, err
	}
	if err := ValidateSchemaVersion(filePath, found, supported); err != nil {
		return 0, err
	}
	return found, nil
}

// ValidateSchemaVersion gives every persisted format the same actionable
// failure when an older binary meets data written by a newer binary.
func ValidateSchemaVersion(filePath string, found, supported int) error {
	if found > supported {
		return fmt.Errorf("cannot load %s: schemaVersion %d is newer than supported schemaVersion %d; upgrade monodev before reading this file", filePath, found, supported)
	}
	return nil
}

// MigrateWorkspaceJSON migrates a workspace-state document to the current
// schema without rebuilding the entire document. Updating only the fields the
// migration owns preserves extension fields from older releases.
func MigrateWorkspaceJSON(filePath string, data []byte) ([]byte, bool, error) {
	version, err := CheckSchemaVersion(filePath, data, WorkspaceSchemaVersion)
	if err != nil {
		return nil, false, err
	}
	if version == WorkspaceSchemaVersion {
		return data, false, nil
	}

	var workspace WorkspaceState
	if err := json.Unmarshal(data, &workspace); err != nil {
		return nil, false, fmt.Errorf("cannot migrate %s: %w", filePath, err)
	}
	workspace.MigrateDeprecatedStack()

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, fmt.Errorf("cannot migrate %s: %w", filePath, err)
	}
	applied, err := json.Marshal(workspace.Applied)
	if err != nil {
		return nil, false, fmt.Errorf("cannot migrate %s: %w", filePath, err)
	}
	appliedStores, err := json.Marshal(workspace.AppliedStores)
	if err != nil {
		return nil, false, fmt.Errorf("cannot migrate %s: %w", filePath, err)
	}
	schemaVersion, err := json.Marshal(WorkspaceSchemaVersion)
	if err != nil {
		return nil, false, fmt.Errorf("cannot migrate %s: %w", filePath, err)
	}

	raw["applied"] = applied
	raw["appliedStores"] = appliedStores
	raw["schemaVersion"] = schemaVersion
	delete(raw, "stack")

	migrated, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("cannot migrate %s: %w", filePath, err)
	}
	return migrated, true, nil
}
