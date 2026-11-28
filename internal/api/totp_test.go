package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/reusingj/orchard2/internal/config"
	"github.com/reusingj/orchard2/internal/testenv"
)

func TestTOTPRFC6238Code(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := totpCode(secret, 1)
	if err != nil {
		t.Fatal(err)
	}
	if code != "287082" {
		t.Fatalf("TOTP code = %q, want %q", code, "287082")
	}
	if !validTOTP(secret, code, time.Unix(59, 0)) {
		t.Fatal("RFC 6238 code should validate within the time window")
	}
	if validTOTP(secret, "000000", time.Unix(59, 0)) {
		t.Fatal("invalid TOTP code validated")
	}
}

func TestEnforcedTOTPSetupCompletesBeforeLogin(t *testing.T) {
	env := testenv.New(t)
	a := New(Deps{Cfg: &config.Config{}, Store: env.Store, MDM: env.MDM})
	if err := a.CreateUser("staff", "long-enough-password", "operator", "Staff"); err != nil {
		t.Fatal(err)
	}
	if err := env.Store.SetSetting(settingTOTPEnforced, "1"); err != nil {
		t.Fatal(err)
	}

	loginBody, err := json.Marshal(map[string]string{"username": "staff", "password": "long-enough-password"})
	if err != nil {
		t.Fatal(err)
	}
	loginRec := httptest.NewRecorder()
	if err := a.login(loginRec, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))); err != nil {
		t.Fatal(err)
	}
	var setup struct {
		SetupRequired bool   `json:"totp_setup_required"`
		SetupToken    string `json:"setup_token"`
		Secret        string `json:"secret"`
		QRData        string `json:"qr_data"`
		OTPAuthURL    string `json:"otpauth_url"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	if !setup.SetupRequired || setup.SetupToken == "" || setup.Secret == "" || setup.QRData == "" || !strings.Contains(setup.OTPAuthURL, "Orchard2") || !strings.Contains(setup.OTPAuthURL, "issuer=Orchard2") {
		t.Fatalf("incomplete forced setup response: %+v", setup)
	}
	if len(loginRec.Result().Cookies()) != 0 {
		t.Fatal("forced setup must not create a signed-in session")
	}

	counter := uint64(time.Now().Unix() / 30)
	code, err := totpCode(setup.Secret, counter)
	if err != nil {
		t.Fatal(err)
	}
	enrollBody, err := json.Marshal(map[string]string{"setup_token": setup.SetupToken, "code": code})
	if err != nil {
		t.Fatal(err)
	}
	enrollRec := httptest.NewRecorder()
	if err := a.enrollTOTPAtLogin(enrollRec, httptest.NewRequest(http.MethodPost, "/api/auth/totp/enroll", bytes.NewReader(enrollBody))); err != nil {
		t.Fatal(err)
	}
	if len(enrollRec.Result().Cookies()) == 0 {
		t.Fatal("successful TOTP setup did not create a session")
	}
	user, err := env.Store.GetUserByName("staff")
	if err != nil {
		t.Fatal(err)
	}
	if !user.TOTPEnabled || user.TOTPSecret != setup.Secret {
		t.Fatal("confirmed TOTP setup was not persisted")
	}

	loginRec = httptest.NewRecorder()
	if err := a.login(loginRec, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))); err != nil {
		t.Fatal(err)
	}
	var challenge struct {
		Required bool   `json:"totp_required"`
		Token    string `json:"login_token"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	if !challenge.Required || challenge.Token == "" || len(loginRec.Result().Cookies()) != 0 {
		t.Fatalf("password step did not leave sign-in pending: %+v", challenge)
	}
	totpBody, _ := json.Marshal(map[string]string{"login_token": challenge.Token, "code": code})
	totpRec := httptest.NewRecorder()
	if err := a.loginTOTP(totpRec, httptest.NewRequest(http.MethodPost, "/api/auth/totp/login", bytes.NewReader(totpBody))); err == nil {
		t.Fatal("reusing a previously accepted authenticator code succeeded")
	}
	nextCode, err := totpCode(setup.Secret, uint64(time.Now().Unix()/30+1))
	if err != nil {
		t.Fatal(err)
	}
	totpBody, _ = json.Marshal(map[string]string{"login_token": challenge.Token, "code": nextCode})
	totpRec = httptest.NewRecorder()
	if err := a.loginTOTP(totpRec, httptest.NewRequest(http.MethodPost, "/api/auth/totp/login", bytes.NewReader(totpBody))); err != nil {
		t.Fatal(err)
	}
	if len(totpRec.Result().Cookies()) == 0 {
		t.Fatal("valid authenticator code did not complete login")
	}
}

func TestUserCanEnableAndDisableTOTP(t *testing.T) {
	env := testenv.New(t)
	a := New(Deps{Cfg: &config.Config{}, Store: env.Store, MDM: env.MDM})
	if err := a.CreateUser("staff", "long-enough-password", "operator", "Staff"); err != nil {
		t.Fatal(err)
	}
	user, err := env.Store.GetUserByName("staff")
	if err != nil {
		t.Fatal(err)
	}
	principal := &Principal{Kind: "session", ID: user.ID, Name: user.Username, Role: user.Role}

	setupRec := httptest.NewRecorder()
	if err := a.beginTOTPSetup(setupRec, httptest.NewRequest(http.MethodPost, "/api/auth/totp/setup", nil), principal); err != nil {
		t.Fatal(err)
	}
	var setup struct {
		SetupToken string `json:"setup_token"`
		Secret     string `json:"secret"`
	}
	if err := json.Unmarshal(setupRec.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	code, err := totpCode(setup.Secret, uint64(time.Now().Unix()/30))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"setup_token": setup.SetupToken, "code": code})
	if err := a.confirmTOTPSetup(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/totp/confirm", bytes.NewReader(body)), principal); err != nil {
		t.Fatal(err)
	}

	body, _ = json.Marshal(map[string]string{"password": "long-enough-password", "code": code})
	if err := env.Store.SetSetting(settingTOTPEnforced, "1"); err != nil {
		t.Fatal(err)
	}
	err = a.disableTOTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/totp/disable", bytes.NewReader(body)), principal)
	apiErr, ok := err.(*Error)
	if !ok || apiErr.Status != http.StatusForbidden || !strings.Contains(apiErr.Msg, "cannot be disabled") {
		t.Fatalf("disabling enforced TOTP error = %v, want explicit 403", err)
	}
	if err := env.Store.SetSetting(settingTOTPEnforced, "0"); err != nil {
		t.Fatal(err)
	}
	if err := a.disableTOTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/totp/disable", bytes.NewReader(body)), principal); err == nil {
		t.Fatal("reusing the setup code to disable TOTP succeeded")
	}
	nextCode, err := totpCode(setup.Secret, uint64(time.Now().Unix()/30+1))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]string{"password": "long-enough-password", "code": nextCode})
	if err := a.disableTOTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/totp/disable", bytes.NewReader(body)), principal); err != nil {
		t.Fatal(err)
	}
	user, err = env.Store.GetUserByName("staff")
	if err != nil {
		t.Fatal(err)
	}
	if user.TOTPEnabled || user.TOTPSecret != "" {
		t.Fatal("TOTP was not disabled")
	}
}
