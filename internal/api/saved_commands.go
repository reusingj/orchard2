package api

import (
	"net/http"
	"strings"

	"github.com/reusingj/orchard2/internal/mdm"
	"github.com/reusingj/orchard2/internal/store"
)

type savedCommandReq struct {
	Name              string         `json:"name"`
	Kind              string         `json:"kind"`
	Command           string         `json:"command"`
	Params            map[string]any `json:"params"`
	QuicksendGroupIDs []int64        `json:"quicksend_group_ids"`
}

func validateSavedCommand(req *savedCommandReq) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return badRequest("name is required")
	}
	if len(req.Name) > 120 {
		return badRequest("name must be at most 120 characters")
	}
	switch req.Kind {
	case "catalog":
		found := false
		for _, spec := range mdm.Catalog {
			if spec.ID == req.Command {
				found = true
				break
			}
		}
		if !found {
			return badRequest("command must be selected from the command catalog")
		}
		if req.Params == nil {
			req.Params = map[string]any{}
		}
	case "custom":
		if _, err := mdm.ParseCustomCommand(req.Command); err != nil {
			return badRequest("invalid custom command: %v", err)
		}
		req.Params = map[string]any{}
	default:
		return badRequest("kind must be catalog or custom")
	}
	if req.QuicksendGroupIDs == nil {
		req.QuicksendGroupIDs = []int64{}
	} else {
		unique := make([]int64, 0, len(req.QuicksendGroupIDs))
		seen := make(map[int64]bool, len(req.QuicksendGroupIDs))
		for _, id := range req.QuicksendGroupIDs {
			if !seen[id] {
				seen[id] = true
				unique = append(unique, id)
			}
		}
		req.QuicksendGroupIDs = unique
	}
	return nil
}

func (a *API) listSavedCommands(w http.ResponseWriter, r *http.Request, p *Principal) error {
	items, err := a.Store.ListSavedCommands()
	if err != nil {
		return err
	}
	type savedCommandSummary struct {
		ID                int64          `json:"id"`
		Name              string         `json:"name"`
		Kind              string         `json:"kind"`
		Command           string         `json:"command,omitempty"`
		Params            map[string]any `json:"params,omitempty"`
		RequestType       string         `json:"request_type,omitempty"`
		QuicksendGroupIDs []int64        `json:"quicksend_group_ids"`
		UpdatedAt         int64          `json:"updated_at"`
	}
	out := make([]savedCommandSummary, 0, len(items))
	for _, item := range items {
		summary := savedCommandSummary{ID: item.ID, Name: item.Name, Kind: item.Kind, UpdatedAt: item.UpdatedAt, QuicksendGroupIDs: item.QuicksendGroupIDs}
		if item.Kind == "custom" {
			if custom, err := mdm.ParseCustomCommand(item.Command); err == nil {
				summary.RequestType, _ = custom["RequestType"].(string)
			}
		}
		if Roles[p.Role] >= PermManage {
			summary.Command, summary.Params = item.Command, item.Params
		} else if item.Kind == "catalog" {
			summary.Command = item.Command
		}
		out = append(out, summary)
	}
	return ok(w, map[string]any{"items": out})
}

func (a *API) createSavedCommand(w http.ResponseWriter, r *http.Request, p *Principal) error {
	var req savedCommandReq
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := validateSavedCommand(&req); err != nil {
		return err
	}
	if err := a.validateSavedCommandGroups(req.QuicksendGroupIDs); err != nil {
		return err
	}
	cmd := &store.SavedCommand{Name: req.Name, Kind: req.Kind, Command: req.Command, Params: req.Params, QuicksendGroupIDs: req.QuicksendGroupIDs}
	if err := a.Store.CreateSavedCommand(cmd); err != nil {
		return err
	}
	a.audit(r, p, "saved_command.create", cmd.Name, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"command": cmd})
	return nil
}

func (a *API) updateSavedCommand(w http.ResponseWriter, r *http.Request, p *Principal) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var req savedCommandReq
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := validateSavedCommand(&req); err != nil {
		return err
	}
	if err := a.validateSavedCommandGroups(req.QuicksendGroupIDs); err != nil {
		return err
	}
	cmd := &store.SavedCommand{ID: id, Name: req.Name, Kind: req.Kind, Command: req.Command, Params: req.Params, QuicksendGroupIDs: req.QuicksendGroupIDs}
	if err := a.Store.UpdateSavedCommand(cmd); err != nil {
		return err
	}
	cmd, err = a.Store.GetSavedCommand(id)
	if err != nil {
		return err
	}
	a.audit(r, p, "saved_command.update", cmd.Name, nil)
	return ok(w, map[string]any{"command": cmd})
}

func (a *API) validateSavedCommandGroups(groupIDs []int64) error {
	groups, err := a.Store.ListGroups()
	if err != nil {
		return err
	}
	valid := make(map[int64]bool, len(groups))
	for _, group := range groups {
		valid[group.ID] = true
	}
	for _, id := range groupIDs {
		if !valid[id] {
			return badRequest("quicksend group %d does not exist", id)
		}
	}
	return nil
}

func (a *API) deleteSavedCommand(w http.ResponseWriter, r *http.Request, p *Principal) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	cmd, err := a.Store.GetSavedCommand(id)
	if err != nil {
		return err
	}
	if err := a.Store.DeleteSavedCommand(id); err != nil {
		return err
	}
	a.audit(r, p, "saved_command.delete", cmd.Name, nil)
	return ok(w, map[string]bool{"deleted": true})
}

func (a *API) runSavedCommand(w http.ResponseWriter, r *http.Request, p *Principal) error {
	d, err := a.device(r)
	if err != nil {
		return err
	}
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	cmd, err := a.Store.GetSavedCommand(id)
	if err != nil {
		return err
	}
	if d.EnrollmentStatus != "enrolled" {
		return badRequest("device is not enrolled")
	}
	source := "manual"
	if p.Kind == "apikey" {
		source = "api"
	}
	meta := mdm.Meta{Source: source, CreatedBy: p.Actor()}
	if cmd.Kind == "catalog" {
		results := a.MDM.SendCatalogCommand(cmd.Command, []string{d.UDID}, mdm.Params(cmd.Params), meta)
		if len(results) != 1 {
			return errf(http.StatusInternalServerError, "command could not be queued")
		}
		if results[0].Error != "" {
			return badRequest("%s", results[0].Error)
		}
		a.audit(r, p, "device.saved_command", d.UDID, map[string]any{"saved_command": cmd.Name})
		return ok(w, results[0])
	}
	payload, err := mdm.ParseCustomCommand(cmd.Command)
	if err != nil {
		return errf(http.StatusInternalServerError, "saved custom command is invalid: %v", err)
	}
	queued, err := a.MDM.EnqueueAndPush(d.UDID, payload, meta)
	if err != nil {
		return err
	}
	a.audit(r, p, "device.saved_command", d.UDID, map[string]any{"saved_command": cmd.Name, "request_type": queued.RequestType})
	return ok(w, queued)
}

func (a *API) sendSavedCommand(w http.ResponseWriter, r *http.Request, p *Principal) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	cmd, err := a.Store.GetSavedCommand(id)
	if err != nil {
		return err
	}
	var req struct {
		DeviceIDs []string `json:"device_ids"`
		GroupIDs  []int64  `json:"group_ids"`
		Quicksend bool     `json:"quicksend"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	action := "saved_command.send"
	if req.Quicksend {
		if len(cmd.QuicksendGroupIDs) == 0 {
			return badRequest("this command has no quicksend groups")
		}
		req.DeviceIDs = nil
		req.GroupIDs = cmd.QuicksendGroupIDs
		action = "saved_command.quicksend"
	}
	udids, err := a.resolveSavedCommandTargets(req.DeviceIDs, req.GroupIDs)
	if err != nil {
		return err
	}
	meta := mdm.Meta{Source: "manual", CreatedBy: p.Actor()}
	if p.Kind == "apikey" {
		meta.Source = "api"
	}
	results, err := queueSavedCommand(a, cmd, udids, meta)
	if err != nil {
		return err
	}
	queued := 0
	for _, result := range results {
		if result.Error == "" {
			queued++
		}
	}
	a.audit(r, p, action, cmd.Name, map[string]int{"devices": len(udids), "queued": queued})
	return ok(w, map[string]any{"results": results})
}

func (a *API) resolveSavedCommandTargets(deviceIDs []string, groupIDs []int64) ([]string, error) {
	if len(deviceIDs) == 0 && len(groupIDs) == 0 {
		return nil, badRequest("select at least one device or group")
	}
	if len(deviceIDs) > 5000 {
		return nil, badRequest("too many devices in one request")
	}
	targets := make(map[string]bool)
	for _, udid := range deviceIDs {
		d, err := a.Store.GetDevice(udid)
		if err != nil {
			return nil, err
		}
		if d.EnrollmentStatus != "enrolled" {
			return nil, badRequest("device %s is not enrolled", udid)
		}
		targets[udid] = true
	}
	for _, groupID := range groupIDs {
		group, err := a.Store.GetGroup(groupID)
		if err != nil {
			return nil, err
		}
		filter := store.DeviceFilter{Status: "enrolled", Limit: 5001}
		if group.Kind != "all" {
			filter.GroupID = groupID
		}
		devices, _, err := a.Store.ListDevices(filter)
		if err != nil {
			return nil, err
		}
		for _, device := range devices {
			targets[device.UDID] = true
		}
	}
	if len(targets) == 0 {
		return nil, badRequest("the selected groups contain no enrolled devices")
	}
	if len(targets) > 5000 {
		return nil, badRequest("too many devices in one request")
	}
	udids := make([]string, 0, len(targets))
	for udid := range targets {
		udids = append(udids, udid)
	}
	return udids, nil
}

func queueSavedCommand(a *API, cmd *store.SavedCommand, udids []string, meta mdm.Meta) ([]mdm.SendResult, error) {
	if cmd.Kind == "catalog" {
		return a.MDM.SendCatalogCommand(cmd.Command, udids, mdm.Params(cmd.Params), meta), nil
	}
	payload, err := mdm.ParseCustomCommand(cmd.Command)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "saved custom command is invalid: %v", err)
	}
	results := make([]mdm.SendResult, 0, len(udids))
	var pushed []string
	for _, udid := range udids {
		queued, err := a.MDM.Enqueue(udid, payload, meta)
		result := mdm.SendResult{UDID: udid}
		if err != nil {
			result.Error = err.Error()
		} else {
			result.CommandUUID = queued.UUID
			pushed = append(pushed, udid)
		}
		results = append(results, result)
	}
	a.MDM.Push(pushed...)
	return results, nil
}
