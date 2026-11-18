package api

import (
	"testing"

	"github.com/reusingj/orchard2/internal/reconcile"
	"github.com/reusingj/orchard2/internal/store"
)

func TestSelectedVPPLicenseTargets(t *testing.T) {
	devices := []*store.Device{
		{UDID: "device-1", SerialNumber: "SERIAL-1"},
		{UDID: "device-2", SerialNumber: "SERIAL-2", UserEnrollment: true, ManagedAppleID: "User@Example.com"},
		{UDID: "device-3", UserEnrollment: true, ManagedAppleID: "user@example.com"},
	}
	targets, err := selectedVPPLicenseTargets(devices)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets.serials) != 1 || targets.serials[0] != "SERIAL-1" {
		t.Fatalf("serials = %v", targets.serials)
	}
	wantUserID := reconcile.VPPClientUserID("User@Example.com")
	if got := targets.userIDs(); len(got) != 1 || got[0] != wantUserID {
		t.Fatalf("user IDs = %v, want %q", got, wantUserID)
	}
}

func TestSelectedVPPLicenseTargetsRequireIdentity(t *testing.T) {
	_, err := selectedVPPLicenseTargets([]*store.Device{{UDID: "device-1", UserEnrollment: true}})
	if err == nil {
		t.Fatal("user-enrolled device without a Managed Apple Account was accepted")
	}
}
