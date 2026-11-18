package api

import "testing"

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
		})
	}
}
