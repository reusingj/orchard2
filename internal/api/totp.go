package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/reusingj/orchard2/internal/mdm"
	"github.com/reusingj/orchard2/internal/store"
	"golang.org/x/crypto/bcrypt"
	"rsc.io/qr"
)

const settingTOTPEnforced = "totp_enforced"

type pendingTOTPSetup struct {
	UserID    int64
	Secret    string
	ExpiresAt time.Time
	Forced    bool
}

func newTOTPSecret() (string, error) {
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random), nil
}

func totpCode(secret string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	if len(key) < 16 {
		return "", errors.New("TOTP secret is too short")
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000), nil
}

func matchingTOTPCounter(secret, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	counter := now.Unix() / 30
	for _, offset := range []int64{-1, 0, 1} {
		current := counter + offset
		if current < 0 {
			continue
		}
		candidate, err := totpCode(secret, uint64(current))
		if err == nil && hmac.Equal([]byte(candidate), []byte(code)) {
			return current, true
		}
	}
	return 0, false
}

func validTOTP(secret, code string, now time.Time) bool {
	_, ok := matchingTOTPCounter(secret, code, now)
	return ok
}

func (a *API) newPendingTOTP(userID int64, forced bool, username string) (map[string]any, error) {
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	a.totpMu.Lock()
	now := time.Now()
	for key, pending := range a.totpPending {
		if now.After(pending.ExpiresAt) {
			delete(a.totpPending, key)
		}
	}
	a.totpPending[token] = pendingTOTPSetup{UserID: userID, Secret: secret, ExpiresAt: now.Add(10 * time.Minute), Forced: forced}
	a.totpMu.Unlock()

	label := url.QueryEscape("Orchard:" + username)
	uri := "otpauth://totp/" + label + "?secret=" + secret + "&issuer=Orchard&algorithm=SHA1&digits=6&period=30"
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return nil, err
	}
	code.Scale = 5
	qrData := "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG())
	return map[string]any{
		"setup_token": token,
		"secret":      secret,
		"otpauth_url": uri,
		"qr_data":     qrData,
	}, nil
}

func (a *API) takePendingTOTP(token string, userID int64, forced bool) (pendingTOTPSetup, bool) {
	a.totpMu.Lock()
	defer a.totpMu.Unlock()
	pending, ok := a.totpPending[token]
	if !ok || time.Now().After(pending.ExpiresAt) || pending.UserID != userID || pending.Forced != forced {
		delete(a.totpPending, token)
		return pendingTOTPSetup{}, false
	}
	return pending, true
}

func (a *API) peekPendingTOTP(token string, forced bool) (pendingTOTPSetup, bool) {
	a.totpMu.Lock()
	defer a.totpMu.Unlock()
	pending, ok := a.totpPending[token]
	if !ok || time.Now().After(pending.ExpiresAt) || pending.Forced != forced {
		delete(a.totpPending, token)
		return pendingTOTPSetup{}, false
	}
	return pending, true
}

func (a *API) confirmPendingTOTP(token, code string, userID int64, forced bool) (*store.User, error) {
	pending, ok := a.takePendingTOTP(token, userID, forced)
	if !ok {
		return nil, errf(401, "two-factor setup expired; sign in again")
	}
	counter, valid := matchingTOTPCounter(pending.Secret, code, time.Now())
	if !valid {
		return nil, errf(401, "invalid authenticator code")
	}
	u, err := a.Store.GetUser(userID)
	if err != nil {
		return nil, err
	}
	u.TOTPSecret = pending.Secret
	u.TOTPEnabled = true
	u.TOTPLastCounter = counter
	if err := a.Store.UpdateUser(u); err != nil {
		return nil, err
	}
	a.totpMu.Lock()
	delete(a.totpPending, token)
	a.totpMu.Unlock()
	return u, nil
}

func (a *API) beginTOTPSetup(w http.ResponseWriter, r *http.Request, p *Principal) error {
	if p.Kind != "session" {
		return badRequest("API keys cannot configure two-factor authentication")
	}
	u, err := a.Store.GetUser(p.ID)
	if err != nil {
		return err
	}
	if u.TOTPEnabled {
		return badRequest("two-factor authentication is already enabled")
	}
	setup, err := a.newPendingTOTP(u.ID, false, u.Username)
	if err != nil {
		return err
	}
	return ok(w, setup)
}

func (a *API) confirmTOTPSetup(w http.ResponseWriter, r *http.Request, p *Principal) error {
	if p.Kind != "session" {
		return badRequest("API keys cannot configure two-factor authentication")
	}
	var req struct {
		SetupToken string `json:"setup_token"`
		Code       string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ip := mdm.ClientIP(r, a.Cfg.TrustProxy)
	if a.tooManyFailures(ip) {
		return errf(429, "too many attempts; try again in a few minutes")
	}
	pending, found := a.peekPendingTOTP(req.SetupToken, false)
	if !found || pending.UserID != p.ID {
		return errf(401, "two-factor setup expired; sign in again")
	}
	if !validTOTP(pending.Secret, req.Code, time.Now()) {
		a.recordFailure(ip)
		return errf(401, "invalid authenticator code")
	}
	if _, err := a.confirmPendingTOTP(req.SetupToken, req.Code, p.ID, false); err != nil {
		return err
	}
	a.audit(r, p, "auth.totp_enabled", "", nil)
	return ok(w, map[string]bool{"totp_enabled": true})
}

func (a *API) enrollTOTPAtLogin(w http.ResponseWriter, r *http.Request) error {
	ip := mdm.ClientIP(r, a.Cfg.TrustProxy)
	if a.tooManyFailures(ip) {
		return errf(429, "too many attempts; try again in a few minutes")
	}
	var req struct {
		SetupToken string `json:"setup_token"`
		Code       string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	pending, found := a.peekPendingTOTP(req.SetupToken, true)
	if !found {
		return errf(401, "two-factor setup expired; sign in again")
	}
	if !validTOTP(pending.Secret, req.Code, time.Now()) {
		a.recordFailure(ip)
		return errf(401, "invalid authenticator code")
	}
	u, err := a.confirmPendingTOTP(req.SetupToken, req.Code, pending.UserID, true)
	if err != nil {
		return err
	}
	out, err := a.startSession(w, r, u)
	if err != nil {
		return err
	}
	_ = a.Store.InsertAudit(&store.AuditEntry{Actor: u.Username, Action: "auth.totp_enabled", IP: ip})
	return ok(w, out)
}

func (a *API) disableTOTP(w http.ResponseWriter, r *http.Request, p *Principal) error {
	if p.Kind != "session" {
		return badRequest("API keys cannot configure two-factor authentication")
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ip := mdm.ClientIP(r, a.Cfg.TrustProxy)
	if a.tooManyFailures(ip) {
		return errf(429, "too many attempts; try again in a few minutes")
	}
	u, err := a.Store.GetUser(p.ID)
	if err != nil {
		return err
	}
	if !u.TOTPEnabled || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		a.recordFailure(ip)
		return badRequest("password or authenticator code is incorrect")
	}
	counter, valid := matchingTOTPCounter(u.TOTPSecret, req.Code, time.Now())
	if valid {
		valid, err = a.Store.UseTOTPCounter(u.ID, counter)
		if err != nil {
			return err
		}
	}
	if !valid {
		a.recordFailure(ip)
		return badRequest("password or authenticator code is incorrect")
	}
	u.TOTPSecret = ""
	u.TOTPEnabled = false
	u.TOTPLastCounter = -1
	if err := a.Store.UpdateUser(u); err != nil {
		return err
	}
	a.audit(r, p, "auth.totp_disabled", "", nil)
	return ok(w, map[string]bool{"totp_enabled": false})
}
