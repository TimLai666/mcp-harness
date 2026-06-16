package harness

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	store, err := DefaultStore()
	if err != nil {
		return nil, err
	}
	return store.ListApprovals()
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
	store, err := DefaultStore()
	if err != nil {
		return err
	}
	return store.SaveApproval(record)
}

func LoadApproval(id string) (ApprovalRecord, error) {
	store, err := DefaultStore()
	if err != nil {
		return ApprovalRecord{}, err
	}
	return store.LoadApproval(id)
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
