package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/reusingj/orchard2/internal/config"
	"github.com/reusingj/orchard2/internal/mdm"
	"github.com/reusingj/orchard2/internal/testenv"
)

func TestSetupStoresOrganizationNameAbbreviation(t *testing.T) {
	env := testenv.New(t)
	a := New(Deps{Cfg: &config.Config{}, Store: env.Store, MDM: env.MDM})
	body, err := json.Marshal(map[string]string{
		"setup_code":            a.SetupToken(),
		"username":              "admin",
		"password":              "long-enough-password",
		"org_name_abbreviation": "Orchard",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	if err := a.setup(rec, req); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := env.Store.GetSetting(mdm.SettingOrgNameAbbreviation, ""); got != "Orchard" {
		t.Fatalf("stored org name abbreviation = %q, want %q", got, "Orchard")
	}
}
