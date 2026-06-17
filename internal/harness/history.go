package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	snapshotMaxFiles      = 2000
	snapshotMaxFileBytes  = 1 << 20
	snapshotMaxTotalBytes = 8 << 20
	diffMaxChars          = 200000
)

var snapshotSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".next":        true,
	".turbo":       true,
}

func CaptureWorkspaceSnapshot(root string) (WorkspaceSnapshot, error) {
	snapshot := WorkspaceSnapshot{Files: map[string]SnapshotFile{}}
	total := int64(0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil {
			return nil
		}
		if path == root {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if snapshotSkipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		rel := Rel(root, path)
		if len(snapshot.Files) >= snapshotMaxFiles {
			snapshot.Truncated = true
			snapshot.OmittedPaths = append(snapshot.OmittedPaths, rel)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.Size() > snapshotMaxFileBytes || total+info.Size() > snapshotMaxTotalBytes {
			snapshot.Truncated = true
			snapshot.OmittedPaths = append(snapshot.OmittedPaths, rel)
			snapshot.Files[rel] = SnapshotFile{Type: "omitted", Size: info.Size()}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if IsBinary(data) {
			snapshot.Files[rel] = SnapshotFile{Type: "binary", Size: info.Size()}
			return nil
		}
		total += int64(len(data))
		snapshot.Files[rel] = SnapshotFile{Type: "text", Size: info.Size(), Content: string(data)}
		return nil
	})
	sort.Strings(snapshot.OmittedPaths)
	return snapshot, err
}

func DiffSnapshots(before, after WorkspaceSnapshot) (string, bool) {
	paths := map[string]bool{}
	for path := range before.Files {
		paths[path] = true
	}
	for path := range after.Files {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)

	var b strings.Builder
	truncated := false
	appendLimited := func(text string) {
		if truncated {
			return
		}
		if b.Len()+len(text) > diffMaxChars {
			remaining := diffMaxChars - b.Len()
			if remaining > 0 {
				b.WriteString(text[:remaining])
			}
			truncated = true
			return
		}
		b.WriteString(text)
	}
	for _, path := range ordered {
		oldFile, oldOK := before.Files[path]
		newFile, newOK := after.Files[path]
		if oldOK && newOK && oldFile.Type == newFile.Type && oldFile.Content == newFile.Content {
			continue
		}
		appendLimited(fmt.Sprintf("diff --git a/%s b/%s\n", filepath.ToSlash(path), filepath.ToSlash(path)))
		if !oldOK {
			appendLimited("new file mode 100644\n")
		}
		if !newOK {
			appendLimited("deleted file mode 100644\n")
		}
		if (oldOK && oldFile.Type != "text") || (newOK && newFile.Type != "text") {
			appendLimited(fmt.Sprintf("# non-text or omitted file changed: %s\n\n", filepath.ToSlash(path)))
			continue
		}
		if oldOK {
			appendLimited(fmt.Sprintf("--- a/%s\n", filepath.ToSlash(path)))
		} else {
			appendLimited("--- /dev/null\n")
		}
		if newOK {
			appendLimited(fmt.Sprintf("+++ b/%s\n", filepath.ToSlash(path)))
		} else {
			appendLimited("+++ /dev/null\n")
		}
		emitUnifiedHunks(splitDiffLines(oldFile.Content), splitDiffLines(newFile.Content), 3, appendLimited)
		appendLimited("\n")
	}
	if truncated {
		b.WriteString("\n# diff truncated\n")
	}
	return b.String(), truncated
}

func SaveWorkspaceVersion(sessionID string, workspace Workspace, step int, tool, label string, snapshot WorkspaceSnapshot) (WorkspaceVersion, error) {
	version := WorkspaceVersion{
		ID:            historyID(sessionID, workspace.Root, step, tool, label, time.Now()),
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		SessionID:     sessionID,
		WorkspaceRoot: workspace.Root,
		Mode:          workspace.Mode,
		Step:          step,
		Tool:          tool,
		Label:         label,
		Snapshot:      snapshot,
	}
	if workspace.Project != nil {
		version.ProjectID = workspace.Project.ID
		version.ProjectName = workspace.Project.Name
	}
	store, err := DefaultStore()
	if err != nil {
		return WorkspaceVersion{}, err
	}
	return version, store.SaveWorkspaceVersion(version)
}

func AppendHistoryEvent(event HistoryEvent) error {
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.AppendHistoryEvent(event)
}

func ListHistoryEvents(projectID, sessionID string, limit int, includeDiff bool) ([]HistoryEvent, error) {
	store, err := DefaultStore()
	if err != nil {
		return nil, err
	}
	return store.ListHistoryEvents(projectID, sessionID, limit, includeDiff)
}

func GetHistoryEvent(id string) (HistoryEvent, error) {
	store, err := DefaultStore()
	if err != nil {
		return HistoryEvent{}, err
	}
	return store.GetHistoryEvent(id)
}

func LoadWorkspaceVersion(id string) (WorkspaceVersion, error) {
	store, err := DefaultStore()
	if err != nil {
		return WorkspaceVersion{}, err
	}
	return store.LoadWorkspaceVersion(id)
}

func RestoreWorkspaceVersion(root, versionID string) (WorkspaceVersion, string, bool, error) {
	version, err := LoadWorkspaceVersion(versionID)
	if err != nil {
		return WorkspaceVersion{}, "", false, err
	}
	if filepath.Clean(version.WorkspaceRoot) != filepath.Clean(root) {
		return WorkspaceVersion{}, "", false, errors.New("version belongs to a different workspace")
	}
	before, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return WorkspaceVersion{}, "", false, err
	}
	if err := ApplyWorkspaceSnapshot(root, version.Snapshot); err != nil {
		return WorkspaceVersion{}, "", false, err
	}
	after, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return WorkspaceVersion{}, "", false, err
	}
	diff, truncated := DiffSnapshots(before, after)
	return version, diff, truncated, nil
}

func PreviewRestoreWorkspaceVersion(root, versionID string) (WorkspaceVersion, string, bool, error) {
	version, err := LoadWorkspaceVersion(versionID)
	if err != nil {
		return WorkspaceVersion{}, "", false, err
	}
	if filepath.Clean(version.WorkspaceRoot) != filepath.Clean(root) {
		return WorkspaceVersion{}, "", false, errors.New("version belongs to a different workspace")
	}
	current, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return WorkspaceVersion{}, "", false, err
	}
	diff, truncated := DiffSnapshots(current, version.Snapshot)
	return version, diff, truncated, nil
}

func ApplyWorkspaceSnapshot(root string, snapshot WorkspaceSnapshot) error {
	current, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return err
	}
	for path, file := range current.Files {
		if file.Type != "text" {
			continue
		}
		if _, ok := snapshot.Files[path]; ok {
			continue
		}
		full, err := ResolveInside(root, path)
		if err != nil {
			return err
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for path, file := range snapshot.Files {
		if file.Type != "text" {
			continue
		}
		full, err := ResolveInside(root, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func NewHistoryEvent(sessionID string, workspace Workspace, step int, call HarnessCall, observation Observation, beforeVersion, afterVersion WorkspaceVersion, diff string, diffTruncated bool) HistoryEvent {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := HistoryEvent{
		ID:            historyID(sessionID, workspace.Root, step, call.Tool, observation.Status, time.Now()),
		Timestamp:     now,
		SessionID:     sessionID,
		WorkspaceRoot: workspace.Root,
		Mode:          workspace.Mode,
		Step:          step,
		Tool:          call.Tool,
		Status:        observation.Status,
		Args:          approvalComparableArgs(call.Args),
		Error:         observation.Error,
		BeforeVersion: beforeVersion.ID,
		AfterVersion:  afterVersion.ID,
		Diff:          diff,
		DiffTruncated: diffTruncated,
	}
	if beforeVersion.Snapshot.Truncated || afterVersion.Snapshot.Truncated {
		event.SnapshotNotice = "snapshot truncated; large, binary, or skipped paths may not be restorable"
	}
	if workspace.Project != nil {
		event.ProjectID = workspace.Project.ID
		event.ProjectName = workspace.Project.Name
	}
	return event
}

func historyID(parts ...any) string {
	raw := make([]string, 0, len(parts))
	for _, part := range parts {
		raw = append(raw, fmt.Sprint(part))
	}
	sum := sha256.Sum256([]byte(strings.Join(raw, "|")))
	return "hist-" + hex.EncodeToString(sum[:])[:18]
}

func splitDiffLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
