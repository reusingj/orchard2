package store

import (
	"errors"
	"testing"
)

func TestSavedCommandCRUD(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	cmd := &SavedCommand{Name: "Inventory", Kind: "catalog", Command: "DeviceInformation", Params: map[string]any{"Queries": []any{"BatteryLevel"}}}
	if err := s.CreateSavedCommand(cmd); err != nil {
		t.Fatal(err)
	}
	if cmd.ID == 0 || cmd.CreatedAt == 0 {
		t.Fatalf("command was not assigned database metadata: %+v", cmd)
	}
	got, err := s.GetSavedCommand(cmd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != cmd.Name || got.Command != cmd.Command || got.Params["Queries"] == nil {
		t.Fatalf("saved command did not round-trip: %+v", got)
	}

	if err := s.CreateSavedCommand(&SavedCommand{Name: "inventory", Kind: "custom", Command: `{"RequestType":"DeviceInformation"}`}); err == nil {
		t.Fatal("duplicate case-insensitive name was accepted")
	}
	got.Name = "Updated inventory"
	if err := s.UpdateSavedCommand(got); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListSavedCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != got.Name {
		t.Fatalf("unexpected saved commands: %+v", items)
	}
	if err := s.DeleteSavedCommand(got.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSavedCommand(got.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSavedCommand error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteSavedCommand(got.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteSavedCommand error = %v, want ErrNotFound", err)
	}
}
