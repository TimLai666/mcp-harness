package harness

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// ApprovalStore reads and writes approvals for one tenant (zero value = default
// owner / single-tenant).
type ApprovalStore struct{ Owner string }

func (s ApprovalStore) owner() string { return NormalizeOwner(s.Owner) }

func (s ApprovalStore) Create(sessionID string, workspace Workspace, call HarnessCall, reason string) (ApprovalRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := approvalID(sessionID, call)
	project := ""
	if workspace.Project != nil {
		project = workspace.Project.ID
	}
	record := ApprovalRecord{
		ID:        id,
		Owner:     s.owner(),
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

func (s ApprovalStore) List() ([]ApprovalRecord, error) {
	store, err := DefaultStoreFor(s.owner())
	if err != nil {
		return nil, err
	}
	records, err := store.ListApprovals()
	if err != nil {
		return nil, err
	}
	owner := s.owner()
	for i := range records {
		records[i].Owner = owner
	}
	return records, nil
}

func (s ApprovalStore) SetStatus(id string, status ApprovalStatus) (ApprovalRecord, error) {
	if status != ApprovalApproved && status != ApprovalRejected {
		return ApprovalRecord{}, fmt.Errorf("invalid approval status: %s", status)
	}
	record, err := LoadApprovalFor(s.owner(), id)
	if err != nil {
		return ApprovalRecord{}, err
	}
	record.Owner = s.owner()
	record.Status = status
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return record, SaveApproval(record)
}

func (s ApprovalStore) IsApproved(id, sessionID, tool string, args map[string]any) bool {
	record, err := LoadApprovalFor(s.owner(), id)
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
	store, err := DefaultStoreFor(NormalizeOwner(record.Owner))
	if err != nil {
		return err
	}
	return store.SaveApproval(record)
}

func LoadApproval(id string) (ApprovalRecord, error) { return LoadApprovalFor(DefaultOwner, id) }

func LoadApprovalFor(owner, id string) (ApprovalRecord, error) {
	store, err := DefaultStoreFor(owner)
	if err != nil {
		return ApprovalRecord{}, err
	}
	record, err := store.LoadApproval(id)
	if err != nil {
		return record, err
	}
	record.Owner = NormalizeOwner(owner)
	return record, nil
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
