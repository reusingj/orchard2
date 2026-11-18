import { html, $, $$, api, can, modal, toast, toastError, confirmDialog, emptyState } from "../lib.js";
import { catalog } from "../components.js";

export async function render({ root }) {
  const cat = await catalog();
  const specs = cat.items;
  const specById = Object.fromEntries(specs.map((s) => [s.id, s]));

  const load = async () => {
    const res = await api.get("/api/saved-commands");
    root.innerHTML = html`<div class="page-head"><div><h1>Commands</h1><p class="sub">Save reusable command templates from the catalog or define your own MDM command payloads.</p></div>
      ${can("manage") ? html`<button class="btn btn-primary" id="new-command">New command</button>` : ""}</div>
      <section class="panel" id="saved-list">${res.items.length ? html`<div class="table-wrap"><table class="table"><thead><tr><th>Name</th><th>Type</th><th>Command</th><th>Updated</th><th></th></tr></thead><tbody>
        ${res.items.map((item) => html`<tr><td><strong>${item.name}</strong></td><td>${item.kind === "custom" ? "Custom MDM" : "Catalog template"}</td>
          <td>${item.kind === "custom" ? (item.request_type || customRequestType(item.command)) : (specById[item.command]?.label || item.command)}</td><td class="nowrap">${new Date(item.updated_at * 1000).toLocaleDateString()}</td>
          <td class="right nowrap">${can("act") ? html`<button class="btn btn-sm btn-primary" data-run="${item.id}">Send</button>` : ""}
            ${can("manage") ? html`<button class="btn btn-sm" data-edit="${item.id}">Edit</button><button class="btn btn-sm btn-ghost" data-delete="${item.id}">Delete</button>` : ""}</td></tr>`)}</tbody></table></div>` :
        emptyState("No saved commands", "Create a template from the command catalog or add your own MDM command.", can("manage") ? html`<button class="btn btn-primary" id="new-command-empty">New command</button>` : "")}</section>`.s;
    $("#new-command", root)?.addEventListener("click", () => editCommand());
    $("#new-command-empty", root)?.addEventListener("click", () => editCommand());
    $$("[data-edit]", root).forEach((b) => b.addEventListener("click", () => editCommand(res.items.find((i) => i.id === Number(b.dataset.edit)))));
    $$("[data-delete]", root).forEach((b) => b.addEventListener("click", async () => {
      const item = res.items.find((i) => i.id === Number(b.dataset.delete));
      if (!(await confirmDialog("Delete saved command?", `“${item.name}” will be permanently deleted.`, { confirmLabel: "Delete", danger: true }))) return;
      try { await api.del(`/api/saved-commands/${item.id}`); toast("Command deleted"); await load(); } catch (e) { toastError(e); }
    }));
    $$("[data-run]", root).forEach((b) => b.addEventListener("click", () => runCommand(res.items.find((i) => i.id === Number(b.dataset.run)))));
  };

  const editCommand = (item) => {
    const isEdit = !!item;
    const ctl = modal({
      title: isEdit ? "Edit saved command" : "Save command",
      wide: true,
      body: html`<label class="field"><span>Name</span><input name="name" maxlength="120" required value="${item?.name || ""}" placeholder="e.g. Refresh device inventory"></label>
        <label class="field"><span>Command type</span><select name="kind"><option value="catalog" ${!item || item.kind === "catalog" ? "selected" : ""}>Command catalog template</option><option value="custom" ${item?.kind === "custom" ? "selected" : ""}>Custom MDM command</option></select></label>
        <div data-kind="catalog" ${item?.kind === "custom" ? 'style="display:none"' : ""}>
          <label class="field"><span>Catalog command</span><select name="catalog-command">${specs.map((s) => html`<option value="${s.id}" ${item?.kind === "catalog" && item.command === s.id ? "selected" : ""}>${s.category} — ${s.label}</option>`)}</select></label>
          <label class="field"><span>Parameters (JSON object)</span><textarea name="params" class="code" rows="6" placeholder='{"Message":"Please return this device"}'>${item?.kind === "catalog" ? JSON.stringify(item.params || {}, null, 2) : "{}"}</textarea></label>
          <p class="hint">Enter the parameter keys and values expected by the selected catalog command. Some commands require device-specific records.</p>
        </div>
        <div data-kind="custom" ${item?.kind !== "custom" ? 'style="display:none"' : ""}>
          <label class="field"><span>MDM command dictionary (JSON or plist)</span><textarea name="custom-command" class="code" rows="10" placeholder='{"RequestType":"DeviceInformation","Queries":["BatteryLevel"]}'>${item?.kind === "custom" ? item.command : ""}</textarea></label>
          <div class="callout warn"><p>Custom MDM commands are sent directly to the selected device. Review the request type and payload carefully. Remote wipe and user-deletion requests are blocked.</p></div>
        </div>`,
      actions: [{ id: "cancel", label: "Cancel" }, { id: "save", label: "Save command", kind: "primary", submit: true, onClick: async () => {
        const form = $("form", ctl.el);
        const kind = form.elements.kind.value;
        const data = { name: form.elements.name.value, kind, command: "", params: {} };
        if (kind === "catalog") {
          data.command = form.elements["catalog-command"].value;
          try {
            data.params = JSON.parse(form.elements.params.value || "{}");
            if (!data.params || Array.isArray(data.params) || typeof data.params !== "object") throw new Error("Parameters must be a JSON object.");
          } catch (e) { toast(e.message === "Parameters must be a JSON object." ? e.message : "Parameters must be valid JSON.", "err"); return false; }
        } else {
          data.command = form.elements["custom-command"].value;
        }
        await (isEdit ? api.put(`/api/saved-commands/${item.id}`, data) : api.post("/api/saved-commands", data));
        toast(isEdit ? "Command updated" : "Command saved");
        await load();
      } }],
    });
    $("select[name=kind]", ctl.el).addEventListener("change", (e) => {
      $$("[data-kind]", ctl.el).forEach((el) => { el.style.display = el.dataset.kind === e.target.value ? "" : "none"; });
    });
  };

  const runCommand = async (item) => {
    try {
      const res = await api.get("/api/devices?status=enrolled&limit=1000");
      if (!res.items.length) { toast("No enrolled devices available", "err"); return; }
      let ctl;
      ctl = modal({
        title: `Send “${item.name}”`,
        body: html`<label class="field"><span>Enrolled device</span><select name="device">${res.items.map((d) => html`<option value="${d.udid}">${d.device_name || d.udid} — ${d.serial_number || d.udid}</option>`)}</select></label>
          <p class="hint">The command is queued for the device and delivered when it checks in.</p>`,
        actions: [{ id: "cancel", label: "Cancel" }, { id: "send", label: "Send command", kind: "danger", submit: true, onClick: async () => {
          const udid = $("form", ctl.el).elements.device.value;
          const device = res.items.find((d) => d.udid === udid);
          if (!(await confirmDialog("Queue this command?", `“${item.name}” will be sent to ${device?.device_name || "the selected device"} when it checks in.`, { confirmLabel: "Queue command", danger: true }))) return false;
          await api.post(`/api/devices/${encodeURIComponent(udid)}/saved-commands/${item.id}`);
          toast("Command queued");
        } }],
      });
    } catch (e) { toastError(e); }
  };

  await load();
}

function customRequestType(command) {
  try { return JSON.parse(command).RequestType || "Custom MDM command"; } catch { return "Custom MDM command"; }
}
