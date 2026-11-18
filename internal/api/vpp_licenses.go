package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/reusingj/orchard2/internal/mdm"
	"github.com/reusingj/orchard2/internal/reconcile"
	"github.com/reusingj/orchard2/internal/store"
	"github.com/reusingj/orchard2/internal/vpp"
)

const vppAssignmentBatchSize = 100

type vppLicenseTargets struct {
	serials []string
	users   map[string]vpp.User
}

func newVPPLicenseTargets() vppLicenseTargets {
	return vppLicenseTargets{users: map[string]vpp.User{}}
}

func (t *vppLicenseTargets) addSerial(serial string) {
	if serial != "" {
		t.serials = append(t.serials, serial)
	}
}

func (t *vppLicenseTargets) addUser(managedAppleID string) {
	managedAppleID = strings.TrimSpace(managedAppleID)
	if managedAppleID == "" {
		return
	}
	id := reconcile.VPPClientUserID(managedAppleID)
	t.users[id] = vpp.User{ClientUserID: id, Email: managedAppleID, ManagedAppleID: managedAppleID}
}

func (t *vppLicenseTargets) normalize() {
	sort.Strings(t.serials)
	t.serials = uniqueStrings(t.serials)
}

func (t vppLicenseTargets) userIDs() []string {
	ids := make([]string, 0, len(t.users))
	for id := range t.users {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func uniqueStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

func selectedVPPLicenseTargets(devices []*store.Device) (vppLicenseTargets, error) {
	targets := newVPPLicenseTargets()
	for _, d := range devices {
		userBased := d.UserEnrollment || (d.SerialNumber == "" && d.ManagedAppleID != "")
		if userBased {
			if strings.TrimSpace(d.ManagedAppleID) == "" {
				return targets, fmt.Errorf("device %s has no Managed Apple Account", d.UDID)
			}
			targets.addUser(d.ManagedAppleID)
		} else if d.SerialNumber == "" {
			return targets, fmt.Errorf("device %s has no serial number", d.UDID)
		} else {
			targets.addSerial(d.SerialNumber)
		}
	}
	targets.normalize()
	return targets, nil
}

func allVPPLicenseTargets(devices []*store.Device) vppLicenseTargets {
	targets := newVPPLicenseTargets()
	for _, d := range devices {
		targets.addSerial(d.SerialNumber)
		targets.addUser(d.ManagedAppleID)
	}
	targets.normalize()
	return targets
}

func (a *API) getVPPAsset(tokenID int64, adamID, pricing string) (*store.VPPToken, *store.VPPAsset, error) {
	token, err := a.Store.GetVPPToken(tokenID)
	if err != nil {
		return nil, nil, err
	}
	assets, err := a.Store.ListVPPAssets(tokenID)
	if err != nil {
		return nil, nil, err
	}
	for _, asset := range assets {
		if asset.AdamID == adamID && asset.PricingParam == pricing {
			return token, asset, nil
		}
	}
	return nil, nil, store.ErrNotFound
}

func splitVPPBatches(ids []string, fn func([]string) (*vpp.EventResponse, error)) ([]string, error) {
	var events []string
	for i := 0; i < len(ids); i += vppAssignmentBatchSize {
		end := min(i+vppAssignmentBatchSize, len(ids))
		event, err := fn(ids[i:end])
		if err != nil {
			return events, err
		}
		if event != nil && event.EventID != "" {
			events = append(events, event.EventID)
		}
	}
	return events, nil
}

func (a *API) applyVPPAssetAction(ctx context.Context, token *store.VPPToken, asset *store.VPPAsset, targets vppLicenseTargets, action string) ([]string, error) {
	userIDs := targets.userIDs()
	if action == "associate" && len(targets.serials) > 0 && !asset.DeviceAssignable {
		return nil, badRequest("this asset cannot be assigned to devices; select a device with User Enrollment instead")
	}
	if action == "disassociate" && !asset.Revocable {
		return nil, badRequest("licenses for this asset cannot be revoked")
	}
	if len(targets.serials)+len(userIDs) == 0 {
		return nil, nil
	}

	client := vpp.New(a.Cfg.VPPURL, token.Token)
	var events []string
	var err error
	if action == "associate" {
		events, err = splitVPPBatches(targets.serials, func(serials []string) (*vpp.EventResponse, error) {
			return client.Associate(ctx, asset.AdamID, asset.PricingParam, serials)
		})
	} else {
		events, err = splitVPPBatches(targets.serials, func(serials []string) (*vpp.EventResponse, error) {
			return client.Disassociate(ctx, asset.AdamID, asset.PricingParam, serials)
		})
	}
	if err != nil {
		return events, errf(http.StatusBadGateway, "Apps and Books %s failed: %v", action, err)
	}

	for i := 0; i < len(userIDs); i += vppAssignmentBatchSize {
		end := min(i+vppAssignmentBatchSize, len(userIDs))
		chunk := userIDs[i:end]
		var createErr error
		if action == "associate" {
			users := make([]vpp.User, 0, len(chunk))
			for _, id := range chunk {
				users = append(users, targets.users[id])
			}
			_, createErr = client.CreateUsers(ctx, users)
		}
		var event *vpp.EventResponse
		if action == "associate" {
			event, err = client.AssociateUsers(ctx, asset.AdamID, asset.PricingParam, chunk)
		} else {
			event, err = client.DisassociateUsers(ctx, asset.AdamID, asset.PricingParam, chunk)
		}
		if err != nil {
			if createErr != nil {
				return events, errf(http.StatusBadGateway, "Apps and Books user registration failed: %v; assignment failed: %v", createErr, err)
			}
			return events, errf(http.StatusBadGateway, "Apps and Books %s for user accounts failed: %v", action, err)
		}
		if event != nil && event.EventID != "" {
			events = append(events, event.EventID)
		}
	}
	return events, nil
}

func (a *API) vppAssetAssignmentAction(w http.ResponseWriter, r *http.Request, p *Principal) error {
	tokenID, err := pathID(r, "token_id")
	if err != nil {
		return err
	}
	adamID := r.PathValue("adam_id")
	if id, parseErr := strconv.ParseInt(adamID, 10, 64); parseErr != nil || id <= 0 {
		return badRequest("invalid adam_id")
	}
	pricing := r.PathValue("pricing")
	if pricing == "" {
		return badRequest("pricing parameter is required")
	}
	action := r.PathValue("action")
	if action != "associate" && action != "disassociate" {
		return badRequest("action must be associate or disassociate")
	}
	token, asset, err := a.getVPPAsset(tokenID, adamID, pricing)
	if err != nil {
		return err
	}
	var req struct {
		UDIDs []string `json:"udids"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if len(req.UDIDs) == 0 || len(req.UDIDs) > 1000 {
		return badRequest("select between 1 and 1000 devices")
	}
	devices := make([]*store.Device, 0, len(req.UDIDs))
	seen := map[string]bool{}
	for _, udid := range req.UDIDs {
		if seen[udid] {
			continue
		}
		seen[udid] = true
		device, err := a.Store.GetDevice(udid)
		if err != nil {
			return err
		}
		if device.EnrollmentStatus != "enrolled" {
			return badRequest("device %s is not enrolled", udid)
		}
		devices = append(devices, device)
	}
	targets, err := selectedVPPLicenseTargets(devices)
	if err != nil {
		return badRequest("%v", err)
	}
	events, err := a.applyVPPAssetAction(r.Context(), token, asset, targets, action)
	if err != nil {
		return err
	}
	a.audit(r, p, "vpp.license."+action, adamID, map[string]any{"token_id": tokenID, "devices": len(devices), "user_accounts": len(targets.users)})
	return ok(w, map[string]any{"devices": len(devices), "user_accounts": len(targets.users), "event_ids": events})
}

func (a *API) removeAndDisassociateApp(w http.ResponseWriter, r *http.Request, p *Principal) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	app, err := a.Store.GetApp(id)
	if err != nil {
		return err
	}
	if app.Kind != "vpp" && !app.UseVPP {
		return badRequest("this app does not use Apps and Books licenses")
	}
	adamID := strconv.FormatInt(app.ITunesID, 10)
	allAssets, err := a.Store.ListVPPAssets(0)
	if err != nil {
		return err
	}
	tokens := map[int64]*store.VPPToken{}
	var assets []*store.VPPAsset
	for _, asset := range allAssets {
		if asset.AdamID == adamID {
			if !asset.Revocable {
				return badRequest("licenses for %s cannot be revoked", app.Name)
			}
			assets = append(assets, asset)
		}
	}
	if len(assets) == 0 {
		return badRequest("no synced Apps and Books license was found for %s", app.Name)
	}
	devices, _, err := a.Store.ListDevices(store.DeviceFilter{})
	if err != nil {
		return err
	}
	targets := allVPPLicenseTargets(devices)
	removeIDs := map[string]bool{}
	managedIDs, err := a.Store.ManagedAppDeviceIDs(app.BundleID)
	if err != nil {
		return err
	}
	for _, udid := range managedIDs {
		removeIDs[udid] = true
	}
	states, err := a.Store.ListAppStates("", app.ID)
	if err != nil {
		return err
	}
	for _, state := range states {
		if state.Status == "pending" || state.Status == "installing" || state.Status == "installed" {
			removeIDs[state.DeviceID] = true
		}
	}
	var udids []string
	for udid := range removeIDs {
		device, err := a.Store.GetDevice(udid)
		if err != nil {
			return err
		}
		if device.EnrollmentStatus == "enrolled" {
			udids = append(udids, udid)
		}
	}
	sort.Strings(udids)
	var eventIDs []string
	for _, asset := range assets {
		token := tokens[asset.VPPTokenID]
		if token == nil {
			token, err = a.Store.GetVPPToken(asset.VPPTokenID)
			if err != nil {
				return err
			}
			tokens[asset.VPPTokenID] = token
		}
		events, err := a.applyVPPAssetAction(r.Context(), token, asset, targets, "disassociate")
		if err != nil {
			return err
		}
		eventIDs = append(eventIDs, events...)
	}

	if len(udids) > 0 {
		results := a.MDM.SendCatalogCommand("RemoveApplication", udids, mdm.Params{"Identifier": app.BundleID},
			mdm.Meta{Source: "manual", Ref: "app-remove:" + strconv.FormatInt(app.ID, 10), CreatedBy: p.Actor()})
		var failures []string
		for _, result := range results {
			if result.Error != "" {
				failures = append(failures, result.UDID+": "+result.Error)
			}
		}
		if len(failures) > 0 {
			failureCount := len(failures)
			if len(failures) > 3 {
				failures = failures[:3]
			}
			return errf(http.StatusBadGateway, "license release was requested, but app removal failed for %d device(s): %s", failureCount, strings.Join(failures, "; "))
		}
	}
	if err := a.Store.DeleteApp(app.ID); err != nil {
		return err
	}
	if app.IPAFile != "" {
		_ = os.Remove(app.IPAFile)
	}
	if a.Reconciler != nil {
		a.Reconciler.TriggerAll()
	}
	a.audit(r, p, "app.removed_and_disassociated", app.Name, map[string]any{"devices": len(udids), "event_ids": eventIDs})
	return ok(w, map[string]any{"ok": true, "devices_queued": len(udids), "event_ids": eventIDs})
}
