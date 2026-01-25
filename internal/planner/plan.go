package planner

// ApplyPlan represents a plan to apply store overlays to a workspace.
type ApplyPlan struct {
	// Stores is the ordered list of stores to apply
	Stores []string

	// Operations is the ordered list of operations to execute
	Operations []Operation

	// Conflicts is a list of detected conflicts (empty if no conflicts)
	Conflicts []Conflict
}

// Operation represents a single filesystem operation to execute.
type Operation struct {
	// Type is the operation type: "create_symlink", "copy", "remove"
	Type string

	// SourcePath is the source path in the store overlay (absolute)
	SourcePath string

	// DestPath is the destination path in the workspace (absolute, for FS operations)
	DestPath string

	// RelPath is the relative path from workspace root (for state tracking)
	RelPath string

	// Store is the ID of the store contributing this operation
	Store string
}

// Conflict represents a conflict detected during planning.
type Conflict struct {
	// Path is the workspace path where the conflict was detected
	Path string

	// Reason is a human-readable explanation of the conflict
	Reason string

	// Existing describes what currently exists at the path
	Existing string

	// Incoming describes what the plan wants to create
	Incoming string
}

// Operation type constants
const (
	OpCreateSymlink = "create_symlink"
	OpCopy          = "copy"
	OpRemove        = "remove"
)

// NewApplyPlan creates a new empty ApplyPlan.
func NewApplyPlan(stores []string) *ApplyPlan {
	return &ApplyPlan{
		Stores:     stores,
		Operations: []Operation{},
		Conflicts:  []Conflict{},
	}
}

// HasConflicts returns true if the plan has any conflicts.
func (p *ApplyPlan) HasConflicts() bool {
	return len(p.Conflicts) > 0
}

// AddOperation adds an operation to the plan.
func (p *ApplyPlan) AddOperation(op Operation) {
	p.Operations = append(p.Operations, op)
}

// AddConflict adds a conflict to the plan.
func (p *ApplyPlan) AddConflict(conflict Conflict) {
	p.Conflicts = append(p.Conflicts, conflict)
}
