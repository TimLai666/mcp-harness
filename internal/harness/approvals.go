package harness

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type ApprovalStore struct{}

func (ApprovalStore) Create(sessionID string, workspace Workspace, call HarnessCall, reason string) (ApprovalRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := approvalID(sessionID, call)
	project := ""
	if workspace.Project != nil {
		project = workspace.Project.ID
	}
	record := ApprovalRecord{
		ID:        id,
		SessionID: sessionID,
		Project:   project,
		Tool:      call.Tool,
		Args:      approvalComparableArgs(call.Args),
		Reason:    reason,
		Status:    ApprovalPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return record, SaveApproval(record)
}

func (ApprovalStore) List() ([]ApprovalRecord, error) {
	dir, err := ApprovalsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var records []ApprovalRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		record, err := LoadApproval(entry.Name()[:len(entry.Name())-len(".json")])
		if err == nil {
			records = append(records, record)
		}
	}
	return records, nil
}

func (ApprovalStore) SetStatus(id string, status ApprovalStatus) (ApprovalRecord, error) {
	if status != ApprovalApproved && status != ApprovalRejected {
		return ApprovalRecord{}, fmt.Errorf("invalid approval status: %s", status)
	}
	record, err := LoadApproval(id)
	if err != nil {
		return ApprovalRecord{}, err
	}
	record.Status = status
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return record, SaveApproval(record)
}

func (ApprovalStore) IsApproved(id, sessionID, tool string, args map[string]any) bool {
	record, err := LoadApproval(id)
	if err != nil {
		return false
	}
	if record.Status != ApprovalApproved || record.SessionID != sessionID || record.Tool != tool {
		return false
	}
	a, _ := json.Marshal(approvalComparableArgs(record.Args))
	b, _ := json.Marshal(approvalComparableArgs(args))
	return string(a) == string(b)
}

func SaveApproval(record ApprovalRecord) error {
	path, err := approvalPath(record.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func LoadApproval(id string) (ApprovalRecord, error) {
	path, err := approvalPath(id)
	if err != nil {
		return ApprovalRecord{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ApprovalRecord{}, fmt.Errorf("approval not found: %s", id)
	}
	if err != nil {
		return ApprovalRecord{}, err
	}
	var record ApprovalRecord
	return record, json.Unmarshal(data, &record)
}

func approvalPath(id string) (string, error) {
	dir, err := ApprovalsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".json"), nil
}

func approvalID(sessionID string, call HarnessCall) string {
	payload, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"index":      call.Index,
		"tool":       call.Tool,
		"args":       approvalComparableArgs(call.Args),
		"time":       time.Now().UnixNano(),
	})
	sum := sha1.Sum(payload)
	return "approval-" + hex.EncodeToString(sum[:])[:16]
}

func approvalComparableArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for key, value := range args {
		if key == "approval_id" {
			continue
		}
		out[key] = value
	}
	return out
}
