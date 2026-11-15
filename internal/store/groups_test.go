package store

import (
	"context"
	"testing"
)

func TestGroupStore_ReplaceAllAndQuery(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer db.Close()

	gs := NewGroupStore(db)
	err = gs.ReplaceAll(ctx, []TechnicianGroup{
		{SDPTechnicianID: "42", SupportGroupID: "301", SupportGroupName: "Входящие"},
		{SDPTechnicianID: "42", SupportGroupID: "601", SupportGroupName: "HELPDESK"},
		{SDPTechnicianID: "99", SupportGroupID: "601", SupportGroupName: "HELPDESK"},
	})
	if err != nil {
		t.Fatalf("ReplaceAll failed: %v", err)
	}

	got42, err := gs.GetGroupIDsByTechnician(ctx, "42")
	if err != nil {
		t.Fatalf("GetGroupIDsByTechnician(42) failed: %v", err)
	}
	if len(got42) != 2 || got42[0] != "301" || got42[1] != "601" {
		t.Fatalf("unexpected groups for 42: %v", got42)
	}

	gotNone, err := gs.GetGroupIDsByTechnician(ctx, "777")
	if err != nil {
		t.Fatalf("GetGroupIDsByTechnician(777) failed: %v", err)
	}
	if len(gotNone) != 0 {
		t.Fatalf("expected 0 groups for 777, got %v", gotNone)
	}

	// ReplaceAll should remove stale memberships
	err = gs.ReplaceAll(ctx, []TechnicianGroup{
		{SDPTechnicianID: "42", SupportGroupID: "901", SupportGroupName: "Разработка"},
	})
	if err != nil {
		t.Fatalf("second ReplaceAll failed: %v", err)
	}
	got42After, err := gs.GetGroupIDsByTechnician(ctx, "42")
	if err != nil {
		t.Fatalf("GetGroupIDsByTechnician(42) after replace failed: %v", err)
	}
	if len(got42After) != 1 || got42After[0] != "901" {
		t.Fatalf("expected [901], got %v", got42After)
	}
}
