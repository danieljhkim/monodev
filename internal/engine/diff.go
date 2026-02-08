package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/danieljhkim/monodev/internal/stores"
)

// Diff compares workspace files against store overlay files.
func (e *Engine) Diff(ctx context.Context, req *DiffRequest) (*DiffResult, error) {
	// Discover workspace
	root, fingerprint, workspacePath, err := e.DiscoverWorkspace(req.CWD)
	if err != nil {
		return nil, err
	}

	// Load or create workspace state
	workspaceState, workspaceID, err := e.LoadOrCreateWorkspaceState(fingerprint, workspacePath, "copy")
	if err != nil {
		return nil, err
	}

	// Determine which store to diff against
	storeID := req.StoreID
	if storeID == "" {
		storeID = workspaceState.ActiveStore
		if storeID == "" {
			return nil, ErrNoActiveStore
		}
	}

	// Resolve store repo (use active store scope or search both)
	var repo stores.StoreRepo
	if storeID == workspaceState.ActiveStore && workspaceState.ActiveStoreScope != "" {
		repo, err = e.storeRepoForScope(workspaceState.ActiveStoreScope)
		if err != nil {
			return nil, err
		}
	} else {
		repo, _, err = e.resolveStoreRepo(storeID, "")
		if err != nil {
			return nil, err
		}
	}

	// Load tracked paths from store
	trackFile, err := repo.LoadTrack(storeID)
	if err != nil {
		if os.IsNotExist(err) {
			// No track.json means no tracked paths
			trackFile = &stores.TrackFile{
				Tracked: []stores.TrackedPath{},
			}
		} else {
			return nil, fmt.Errorf("failed to load track list: %w", err)
		}
	}

	// Get overlay root path
	overlayRoot := repo.OverlayRoot(storeID)

	// Compare each tracked path
	files := make([]DiffFileInfo, 0, len(trackFile.Tracked))
	for _, tracked := range trackFile.Tracked {
		workspacePath := filepath.Join(root, tracked.Path)
		storePath := filepath.Join(overlayRoot, tracked.Path)

		if tracked.Kind == "dir" {
			// For directories, walk and compare all files within
			dirFiles, err := e.compareDirPath(root, overlayRoot, workspacePath, storePath, tracked.Path, req.ShowContent)
			if err != nil {
				return nil, fmt.Errorf("failed to compare directory %s: %w", tracked.Path, err)
			}
			files = append(files, dirFiles...)
		} else {
			fileInfo := e.comparePath(workspacePath, storePath, tracked.Path, tracked.Kind, req.ShowContent)
			files = append(files, fileInfo)
		}
	}

	return &DiffResult{
		WorkspaceID: workspaceID,
		StoreID:     storeID,
		Files:       files,
	}, nil
}

// compareDirPath walks a directory and compares all files within it.
func (e *Engine) compareDirPath(workspaceRoot, overlayRoot, workspaceDir, storeDir, trackedPath string, showContent bool) ([]DiffFileInfo, error) {
	// Collect all file paths from both workspace and store
	fileMap := make(map[string]bool)

	// Walk workspace directory
	workspaceExists, err := e.fs.Exists(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check workspace directory existence: %w", err)
	}
	if workspaceExists {
		err := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				relPath, err := filepath.Rel(workspaceRoot, path)
				if err != nil {
					return err
				}
				fileMap[relPath] = true
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk workspace directory: %w", err)
		}
	}

	// Walk store directory
	storeExists, err := e.fs.Exists(storeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to check store directory existence: %w", err)
	}
	if storeExists {
		err := filepath.Walk(storeDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				relPath, err := filepath.Rel(overlayRoot, path)
				if err != nil {
					return err
				}
				fileMap[relPath] = true
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk store directory: %w", err)
		}
	}

	// Compare each file found (sorted for deterministic output)
	relPaths := make([]string, 0, len(fileMap))
	for relPath := range fileMap {
		relPaths = append(relPaths, relPath)
	}
	sort.Strings(relPaths)

	result := make([]DiffFileInfo, 0, len(relPaths))
	for _, relPath := range relPaths {
		workspacePath := filepath.Join(workspaceRoot, relPath)
		storePath := filepath.Join(overlayRoot, relPath)

		fileInfo := e.comparePath(workspacePath, storePath, relPath, "file", showContent)
		result = append(result, fileInfo)
	}

	return result, nil
}

// comparePath compares a single path between workspace and store overlay.
func (e *Engine) comparePath(workspacePath, storePath, relPath, kind string, showContent bool) DiffFileInfo {
	info := DiffFileInfo{
		Path:  relPath,
		IsDir: kind == "dir",
	}

	// Check existence
	workspaceExists, err := e.fs.Exists(workspacePath)
	if err != nil {
		// Log error but continue with comparison
		workspaceExists = false
	}
	storeExists, err := e.fs.Exists(storePath)
	if err != nil {
		// Log error but continue with comparison
		storeExists = false
	}

	// Determine status based on existence
	if !workspaceExists && !storeExists {
		info.Status = "unchanged"
		return info
	}

	if !storeExists && workspaceExists {
		info.Status = "added"
		if !info.IsDir {
			hash, err := e.hasher.HashFile(workspacePath)
			if err == nil {
				info.WorkspaceHash = hash
			}
		}
		if showContent {
			if workspaceData, err := e.fs.ReadFile(workspacePath); err == nil {
				info.UnifiedDiff, info.Additions, info.Deletions = generateUnifiedDiff(relPath, nil, workspaceData, info.Status)
			}
		}
		return info
	}

	if storeExists && !workspaceExists {
		info.Status = "removed"
		if !info.IsDir {
			hash, err := e.hasher.HashFile(storePath)
			if err == nil {
				info.StoreHash = hash
			}
		}
		if showContent {
			if storeData, err := e.fs.ReadFile(storePath); err == nil {
				info.UnifiedDiff, info.Additions, info.Deletions = generateUnifiedDiff(relPath, storeData, nil, info.Status)
			}
		}
		return info
	}

	// Both exist - compare content (for files only)
	if info.IsDir {
		// For directories, just mark as unchanged if both exist
		info.Status = "unchanged"
		return info
	}

	// Hash both files
	workspaceHash, err := e.hasher.HashFile(workspacePath)
	if err != nil {
		info.Status = "modified"
		return info
	}
	info.WorkspaceHash = workspaceHash

	storeHash, err := e.hasher.HashFile(storePath)
	if err != nil {
		info.Status = "modified"
		return info
	}
	info.StoreHash = storeHash

	// Compare hashes
	if workspaceHash != storeHash {
		info.Status = "modified"
		if showContent {
			workspaceData, workspaceErr := e.fs.ReadFile(workspacePath)
			storeData, storeErr := e.fs.ReadFile(storePath)
			if workspaceErr == nil && storeErr == nil {
				info.UnifiedDiff, info.Additions, info.Deletions = generateUnifiedDiff(relPath, storeData, workspaceData, info.Status)
			}
		}
	} else {
		info.Status = "unchanged"
	}

	return info
}

type lineOp struct {
	kind byte
	text string
	old  int
	new  int
}

func generateUnifiedDiff(relPath string, oldData, newData []byte, status string) (string, int, int) {
	oldBinary := isBinary(oldData)
	newBinary := isBinary(newData)

	oldLabel := "a/" + relPath
	newLabel := "b/" + relPath
	if status == "added" {
		oldLabel = "/dev/null"
	}
	if status == "removed" {
		newLabel = "/dev/null"
	}

	if oldBinary || newBinary {
		return fmt.Sprintf("diff --git a/%s b/%s\nBinary files %s and %s differ\n", relPath, relPath, oldLabel, newLabel), 0, 0
	}

	oldLines := splitLines(string(oldData))
	newLines := splitLines(string(newData))
	ops := diffLineOps(oldLines, newLines)

	additions := 0
	deletions := 0
	for _, op := range ops {
		switch op.kind {
		case '+':
			additions++
		case '-':
			deletions++
		}
	}

	if additions == 0 && deletions == 0 {
		return "", 0, 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", relPath, relPath)
	fmt.Fprintf(&b, "--- %s\n", oldLabel)
	fmt.Fprintf(&b, "+++ %s\n", newLabel)

	hunks := buildHunks(ops, 3)
	for _, hunk := range hunks {
		if len(hunk.ops) == 0 {
			continue
		}

		oldCount := 0
		newCount := 0
		for _, op := range hunk.ops {
			if op.kind != '+' {
				oldCount++
			}
			if op.kind != '-' {
				newCount++
			}
		}

		oldStart := hunk.ops[0].old
		newStart := hunk.ops[0].new
		if oldCount == 0 {
			oldStart = max(oldStart-1, 0)
		}
		if newCount == 0 {
			newStart = max(newStart-1, 0)
		}

		fmt.Fprintf(&b, "@@ -%s +%s @@\n", formatHunkRange(oldStart, oldCount), formatHunkRange(newStart, newCount))
		for _, op := range hunk.ops {
			fmt.Fprintf(&b, "%c%s\n", op.kind, op.text)
		}
	}

	return b.String(), additions, deletions
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	// Remove trailing empty segment from terminal newline to align line-based diff output.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	return bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)
}

func diffLineOps(oldLines, newLines []string) []lineOp {
	n := len(oldLines)
	m := len(newLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	ops := make([]lineOp, 0, n+m)
	i, j := 0, 0
	oldLineNo, newLineNo := 1, 1

	for i < n || j < m {
		if i < n && j < m && oldLines[i] == newLines[j] {
			ops = append(ops, lineOp{
				kind: ' ',
				text: oldLines[i],
				old:  oldLineNo,
				new:  newLineNo,
			})
			i++
			j++
			oldLineNo++
			newLineNo++
			continue
		}

		if j < m && (i == n || dp[i][j+1] >= dp[i+1][j]) {
			ops = append(ops, lineOp{
				kind: '+',
				text: newLines[j],
				old:  oldLineNo,
				new:  newLineNo,
			})
			j++
			newLineNo++
			continue
		}

		ops = append(ops, lineOp{
			kind: '-',
			text: oldLines[i],
			old:  oldLineNo,
			new:  newLineNo,
		})
		i++
		oldLineNo++
	}

	return ops
}

type diffHunk struct {
	ops []lineOp
}

func buildHunks(ops []lineOp, context int) []diffHunk {
	changeIdx := make([]int, 0)
	for i, op := range ops {
		if op.kind != ' ' {
			changeIdx = append(changeIdx, i)
		}
	}
	if len(changeIdx) == 0 {
		return nil
	}

	hunks := make([]diffHunk, 0)
	start := max(changeIdx[0]-context, 0)
	end := min(changeIdx[0]+context, len(ops)-1)

	for _, idx := range changeIdx[1:] {
		nextStart := max(idx-context, 0)
		nextEnd := min(idx+context, len(ops)-1)
		if nextStart <= end {
			end = max(end, nextEnd)
			continue
		}

		hunks = append(hunks, diffHunk{ops: ops[start : end+1]})
		start = nextStart
		end = nextEnd
	}

	hunks = append(hunks, diffHunk{ops: ops[start : end+1]})
	return hunks
}

func formatHunkRange(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}
