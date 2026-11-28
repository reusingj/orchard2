package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/reusingj/orchard2/internal/config"
	"github.com/reusingj/orchard2/internal/mdm"
	"github.com/reusingj/orchard2/internal/testenv"
)

func TestSetupStoresOrganizationNameAbbreviationAndBrandLogo(t *testing.T) {
	env := testenv.New(t)
	a := New(Deps{Cfg: &config.Config{}, Store: env.Store, MDM: env.MDM})
	var logo bytes.Buffer
	pixels := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixels.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	if err := png.Encode(&logo, pixels); err != nil {
		t.Fatal(err)
	}
	encodedLogo := base64.StdEncoding.EncodeToString(logo.Bytes())
	body, err := json.Marshal(map[string]string{
		"setup_code":            a.SetupToken(),
		"username":              "admin",
		"password":              "long-enough-password",
		"org_name_abbreviation": "Orchard",
		"brand_logo":            encodedLogo,
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
	if got := env.Store.GetSetting(brandLogoSetting, ""); got != encodedLogo {
		t.Fatal("setup did not persist the brand logo")
	}

	imageReq := httptest.NewRequest(http.MethodGet, "/api/brand/apple-touch-icon.png", nil)
	imageReq.SetPathValue("asset", "apple-touch-icon.png")
	imageRec := httptest.NewRecorder()
	if err := a.brandImage(imageRec, imageReq); err != nil {
		t.Fatal(err)
	}
	if imageRec.Code != http.StatusOK || imageRec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("brand image response = %d %q, want 200 image/png", imageRec.Code, imageRec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(imageRec.Body.Bytes(), logo.Bytes()) {
		t.Fatal("served brand logo differs from uploaded PNG")
	}
}

func TestPutSettingsUpdatesAndResetsBrandLogo(t *testing.T) {
	env := testenv.New(t)
	a := New(Deps{Cfg: &config.Config{}, Store: env.Store, MDM: env.MDM})
	var logo bytes.Buffer
	pixels := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pixels.SetRGBA(0, 0, color.RGBA{R: 200, G: 20, B: 30, A: 255})
	if err := png.Encode(&logo, pixels); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(logo.Bytes())

	body, _ := json.Marshal(map[string]string{"brand_logo": encoded})
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	if err := a.putSettings(rec, req, &Principal{Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	if got := env.Store.GetSetting(brandLogoSetting, ""); got != encoded {
		t.Fatal("settings update did not replace the brand logo")
	}

	body, _ = json.Marshal(map[string]string{"brand_logo": ""})
	req = httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	if err := a.putSettings(rec, req, &Principal{Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	if got := env.Store.GetSetting(brandLogoSetting, ""); got != "" {
		t.Fatal("settings update did not reset the brand logo")
	}
}
