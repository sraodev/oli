(() => {
  "use strict";
  const section = document.querySelector("#storage-map");
  const title = document.querySelector("#map-title");
  const breadcrumbs = document.querySelector("#map-breadcrumbs");
  const coverage = document.querySelector("#map-coverage");
  const status = document.querySelector("#map-status");
  const rows = document.querySelector("#map-rows");
  const form = document.querySelector("#map-filters");
  const up = document.querySelector("#map-up");
  const previous = document.querySelector("#map-previous");
  const next = document.querySelector("#map-next");
  const pageLabel = document.querySelector("#map-page");
  const fields = Object.fromEntries(["search", "kind", "extension", "min", "max", "before", "after"].map(name => [name, document.querySelector(`#map-${name}`)]));
  let report, nodes = new Map(), children = new Map(), current = "map-0", page = 0;
  const pageSize = 100;
  const bytes = value => {
    let number = Math.max(0, Number(value) || 0), unit = 0;
    const units = ["B", "KiB", "MiB", "GiB", "TiB"];
    while (number >= 1024 && unit < units.length - 1) { number /= 1024; unit++; }
    return `${Number(number.toFixed(2))} ${units[unit]}`;
  };
  const nodeSize = node => Number(node.metrics.allocated_bytes) || 0;
  function navigate(id) {
    if (!nodes.has(id)) return;
    current = id; page = 0; form.reset(); render(); title.focus();
  }
  function navigationButton(button, getID) {
    button.addEventListener("click", () => navigate(getID()));
    button.addEventListener("keydown", event => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        if (!event.repeat && !button.disabled) navigate(getID());
      }
    });
  }
  function descendant(node) {
    let parent = node.parent_id;
    for (let depth = 0; parent && depth < 67; depth++) {
      if (parent === current) return true;
      parent = nodes.get(parent)?.parent_id;
    }
    return false;
  }
  function render() {
    const folder = nodes.get(current);
    if (!folder) return;
    title.textContent = folder.display_path;
    breadcrumbs.replaceChildren();
    const ancestors = [];
    for (let node = folder; node && ancestors.length < 67; node = nodes.get(node.parent_id)) ancestors.unshift(node);
    for (const node of ancestors) {
      const button = document.createElement("button");
      button.type = "button"; button.textContent = node.id === "map-0" ? "Selected scopes" : node.display_path.split("/").pop();
      button.className = "secondary-button";
      if (node.id === current) button.setAttribute("aria-current", "location");
      navigationButton(button, () => node.id); breadcrumbs.append(button);
    }
    up.disabled = !folder.parent_id;
    const direct = children.get(current) || [];
    const hidden = direct.filter(node => node.display_path.split("/").pop().startsWith(".")).reduce((sum, node) => sum + nodeSize(node), 0);
    coverage.textContent = `${bytes(nodeSize(folder))} allocated · ${bytes(folder.metrics.logical_bytes)} logical. Recorded dot-prefixed children: ${bytes(hidden)}. Measured but not mapped here: ${bytes(folder.unmapped_allocated_bytes)} allocated / ${bytes(folder.unmapped_logical_bytes)} logical. ${folder.partial ? "Incomplete coverage: unreadable or skipped size is unknown, not zero." : "No traversal warning for this folder."} ${report.map_nodes_omitted} nodes omitted by the map limit across this report.`;
    const query = fields.search.value.trim().toLowerCase(), kind = fields.kind.value;
    const extension = fields.extension.value.trim().replace(/^\./, "").toLowerCase();
    const min = Number(fields.min.value) * 1048576, max = Number(fields.max.value) * 1048576;
    const before = fields.before.value ? Date.parse(`${fields.before.value}T00:00:00Z`) : null;
    const after = fields.after.value ? Date.parse(`${fields.after.value}T00:00:00Z`) : null;
    rows.replaceChildren();
    if (!form.checkValidity() || (max > 0 && max < min) || (before !== null && after !== null && after >= before) || /[/\\]/.test(extension)) {
      status.textContent = "Choose valid size/date bounds and a file extension without path separators.";
      previous.disabled = true; next.disabled = true; pageLabel.textContent = "No results displayed"; return;
    }
    const filtered = query || kind !== "all" || extension || min > 0 || max > 0 || before !== null || after !== null;
    const matches = [...nodes.values()].filter(node => {
      if (filtered ? !descendant(node) : node.parent_id !== current) return false;
      if (query && !node.display_path.toLowerCase().includes(query)) return false;
      if (kind !== "all" && kind !== node.kind) return false;
      const name = node.display_path.split("/").pop(), dot = name.lastIndexOf(".");
      if (extension && (node.kind !== "file" || dot < 0 || name.slice(dot + 1).toLowerCase() !== extension)) return false;
      const logical = Number(node.metrics.logical_bytes) || 0;
      if (logical < min || (max > 0 && logical > max)) return false;
      if (before !== null || after !== null) {
        const modified = Date.parse(node.modified);
        if (!Number.isFinite(modified) || node.modified.startsWith("0001-") || (before !== null && modified >= before) || (after !== null && modified < after)) return false;
      }
      return true;
    }).sort((a, b) => nodeSize(b) - nodeSize(a) || (a.display_path < b.display_path ? -1 : a.display_path > b.display_path ? 1 : 0));
    const pages = Math.max(1, Math.ceil(matches.length / pageSize)); page = Math.min(page, pages - 1);
    status.textContent = matches.length ? `${matches.length} ${filtered ? "matching descendants (overlapping rows)" : "immediate children"}. Folder totals above are unchanged by filters.` : "No mapped matches. An empty result does not prove the folder is empty; check coverage above.";
    for (const node of matches.slice(page * pageSize, (page + 1) * pageSize)) {
      const row = document.createElement("li");
      const label = document.createElement(node.kind === "directory" ? "button" : "span");
      label.textContent = node.display_path;
      if (node.kind === "directory") { label.type = "button"; label.setAttribute("aria-label", `Open folder ${node.display_path}`); navigationButton(label, () => node.id); }
      const detail = document.createElement("span"); detail.className = "map-row-detail";
      detail.textContent = `${node.kind} · ${bytes(nodeSize(node))} allocated · ${bytes(node.metrics.logical_bytes)} logical${node.partial ? " · partial" : ""}${node.kind === "hardlink" ? " · bytes counted at another location" : ""}`;
      if (node.modified && !node.modified.startsWith("0001-")) detail.textContent += ` · modified ${node.modified.slice(0, 10)} UTC`;
      const bar = document.createElement("progress"); bar.max = Math.max(1, nodeSize(folder)); bar.value = nodeSize(node); bar.setAttribute("aria-label", `${node.display_path}: ${bytes(nodeSize(node))} of current folder`);
      row.append(label, detail, bar); rows.append(row);
    }
    previous.disabled = page === 0; next.disabled = page + 1 >= pages; pageLabel.textContent = `Page ${page + 1} of ${pages} · up to ${pageSize} rows`;
  }
  document.addEventListener("storage-map:report", event => {
    report = event.detail; nodes = new Map(); children = new Map();
    for (const node of report.map_nodes || []) { nodes.set(node.id, node); if (!children.has(node.parent_id)) children.set(node.parent_id, []); children.get(node.parent_id).push(node); }
    section.hidden = !nodes.size; current = "map-0"; page = 0; form.reset(); render();
  });
  form.addEventListener("submit", event => event.preventDefault());
  form.addEventListener("input", () => { page = 0; render(); });
  form.addEventListener("reset", () => { queueMicrotask(() => { page = 0; render(); }); });
  navigationButton(up, () => nodes.get(current)?.parent_id);
  previous.addEventListener("click", () => { page--; render(); });
  next.addEventListener("click", () => { page++; render(); });
})();
