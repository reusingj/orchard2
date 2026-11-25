package api

import (
	"testing"

	"github.com/reusingj/orchard2/internal/store"
	"github.com/reusingj/orchard2/internal/testenv"
)

func TestValidateSavedCommand(t *testing.T) {
	tests := []struct {
		name    string
		req     savedCommandReq
		wantErr bool
	}{
		{name: "catalog command", req: savedCommandReq{Name: "Inventory", Kind: "catalog", Command: "DeviceInformation"}},
		{name: "unknown catalog command", req: savedCommandReq{Name: "Unknown", Kind: "catalog", Command: "NotACommand"}, wantErr: true},
		{name: "custom command", req: savedCommandReq{Name: "Battery", Kind: "custom", Command: `{"RequestType":"DeviceInformation","Queries":["BatteryLevel"]}`}},
		{name: "blocked custom command", req: savedCommandReq{Name: "Wipe", Kind: "custom", Command: `{"RequestType":"EraseDevice"}`}, wantErr: true},
		{name: "missing name", req: savedCommandReq{Kind: "catalog", Command: "DeviceInformation"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSavedCommand(&tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSavedCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.req.QuicksendGroupIDs == nil {
				t.Fatal("validateSavedCommand() left quicksend_group_ids nil")
			}
		})
	}
}

func TestResolveSavedCommandTargetsFromGroups(t *testing.T) {
	env := testenv.New(t)
	first := &store.Group{Name: "First", Kind: "static"}
	second := &store.Group{Name: "Second", Kind: "static"}
	for _, group := range []*store.Group{first, second} {
		if err := env.Store.CreateGroup(group); err != nil {
			t.Fatal(err)
		}
	}
	for _, device := range []struct {
		udid   string
		status string
	}{
		{"device-1", "enrolled"},
		{"device-2", "enrolled"},
		{"pending-device", "pending"},
	} {
		if _, err := env.Store.DB().Exec(`INSERT INTO devices(udid, enrollment_status, created_at, updated_at) VALUES(?,?,1,1)`, device.udid, device.status); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.Store.AddGroupMembers(first.ID, []string{"device-1", "device-2", "pending-device"}); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.AddGroupMembers(second.ID, []string{"device-1"}); err != nil {
		t.Fatal(err)
	}

	a := &API{Deps: Deps{Store: env.Store}}
	udids, err := a.resolveSavedCommandTargets(nil, []int64{first.ID, second.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(udids) != 2 {
		t.Fatalf("resolved %v, want the two distinct enrolled group members", udids)
	}
	found := map[string]bool{}
	for _, udid := range udids {
		found[udid] = true
	}
	if !found["device-1"] || !found["device-2"] || found["pending-device"] {
		t.Fatalf("unexpected group target devices: %v", udids)
	}
}
