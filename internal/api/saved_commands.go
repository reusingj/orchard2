package api

import (
	"net/http"
	"strings"

	"github.com/reusingj/orchard2/internal/mdm"
	"github.com/reusingj/orchard2/internal/store"
)

type savedCommandReq struct {
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Command string         `json:"command"`
	Params  map[string]any `json:"params"`
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
	return nil
}

func (a *API) listSavedCommands(w http.ResponseWriter, r *http.Request, p *Principal) error {
	items, err := a.Store.ListSavedCommands()
	if err != nil {
		return err
	}
	type savedCommandSummary struct {
		ID          int64          `json:"id"`
		Name        string         `json:"name"`
		Kind        string         `json:"kind"`
		Command     string         `json:"command,omitempty"`
		Params      map[string]any `json:"params,omitempty"`
		RequestType string         `json:"request_type,omitempty"`
		UpdatedAt   int64          `json:"updated_at"`
	}
	out := make([]savedCommandSummary, 0, len(items))
	for _, item := range items {
		summary := savedCommandSummary{ID: item.ID, Name: item.Name, Kind: item.Kind, UpdatedAt: item.UpdatedAt}
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
	cmd := &store.SavedCommand{Name: req.Name, Kind: req.Kind, Command: req.Command, Params: req.Params}
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
	cmd := &store.SavedCommand{ID: id, Name: req.Name, Kind: req.Kind, Command: req.Command, Params: req.Params}
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
