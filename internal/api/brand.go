package api

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"net/http"
)

const (
	maxBrandLogoBytes = 1 << 20
	maxBrandLogoSide  = 2048
	brandLogoSetting  = "brand_logo"
)

func (a *API) saveBrandLogo(encoded string) error {
	if len(encoded) > base64.StdEncoding.EncodedLen(maxBrandLogoBytes) {
		return badRequest("brand logo must be a PNG no larger than 1 MB")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return badRequest("brand logo must be a valid PNG image")
	}
	if len(data) == 0 || len(data) > maxBrandLogoBytes {
		return badRequest("brand logo must be a PNG no larger than 1 MB")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxBrandLogoSide || cfg.Height > maxBrandLogoSide {
		return badRequest("brand logo must be a valid PNG with dimensions no larger than 2048 × 2048")
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return badRequest("brand logo must be a valid PNG image")
	}
	return a.Store.SetSetting(brandLogoSetting, base64.StdEncoding.EncodeToString(data))
}

func (a *API) brandImage(w http.ResponseWriter, r *http.Request) error {
	fallback := ""
	switch r.PathValue("asset") {
	case "logo", "favicon":
		fallback = "/static/icon.svg"
	case "apple-touch-icon.png":
		fallback = "/static/apple-touch-icon.png"
	default:
		http.NotFound(w, r)
		return nil
	}
	w.Header().Set("Cache-Control", "no-store")
	encoded := a.Store.GetSetting(brandLogoSetting, "")
	if encoded == "" {
		http.Redirect(w, r, fallback, http.StatusFound)
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode stored brand logo: %w", err)
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, err = w.Write(data)
	return err
}
