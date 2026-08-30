(() => {
  "use strict";

  const elements = {
    activity: document.querySelector("#activity"),
    activityLog: document.querySelector("#activity-log"),
    activityTotal: document.querySelector("#activity-total"),
    authNotice: document.querySelector("#auth-notice"),
    capacityFill: document.querySelector("#capacity-fill"),
    categoryGrid: document.querySelector("#category-grid"),
    categoryTemplate: document.querySelector("#category-template"),
    cleanButton: document.querySelector("#clean-button"),
    cleanProgress: document.querySelector("#clean-progress"),
    cleanProgressDetail: document.querySelector("#clean-progress-detail"),
    cleanProgressPercent: document.querySelector("#clean-progress-percent"),
    cleanProgressTitle: document.querySelector("#clean-progress-title"),
    cleanupDock: document.querySelector("#cleanup-dock"),
    confirmationInput: document.querySelector("#confirmation-input"),
    connectionState: document.querySelector("#connection-state"),
    dayPeriod: document.querySelector("#day-period"),
    diskFree: document.querySelector("#disk-free"),
    diskRing: document.querySelector("#disk-ring"),
    diskRingUsage: document.querySelector("#disk-ring-usage"),
    diskTotal: document.querySelector("#disk-total"),
    diskUsed: document.querySelector("#disk-used"),
    dockRules: document.querySelector("#dock-rules"),
    dockTotal: document.querySelector("#dock-total"),
    emptyState: document.querySelector("#empty-state"),
    measuredTotal: document.querySelector("#measured-total"),
    navRuleCount: document.querySelector("#nav-rule-count"),
    opportunityNote: document.querySelector("#opportunity-note"),
    outcomeDialog: document.querySelector("#outcome-dialog"),
    outcomeDuration: document.querySelector("#outcome-duration"),
    outcomeItems: document.querySelector("#outcome-items"),
    outcomeNote: document.querySelector("#outcome-note"),
    outcomeSelected: document.querySelector("#outcome-selected"),
    profileOptions: document.querySelector("#profile-options"),
    reclaimableCaption: document.querySelector("#reclaimable-caption"),
    reclaimableTotal: document.querySelector("#reclaimable-total"),
    refreshDisk: document.querySelector("#refresh-disk"),
    resultSummary: document.querySelector("#result-summary"),
    ringFree: document.querySelector("#ring-free"),
    scanButton: document.querySelector("#scan-button"),
    scanIssues: document.querySelector("#scan-issues"),
    scanProgress: document.querySelector("#scan-progress"),
    scanProgressDetail: document.querySelector("#scan-progress-detail"),
    scanProgressPanel: document.querySelector("#scan-progress-panel"),
    scanProgressPercent: document.querySelector("#scan-progress-percent"),
    scanProgressTitle: document.querySelector("#scan-progress-title"),
    selectNone: document.querySelector("#select-none"),
    selectSafe: document.querySelector("#select-safe"),
    selectedCount: document.querySelector("#selected-count"),
    selectedTotal: document.querySelector("#selected-total"),
    toast: document.querySelector("#toast"),
    usedPercent: document.querySelector("#used-percent"),
    volumeLabel: document.querySelector("#volume-label"),
  };

  const state = {
    categories: new Map(),
    cleaning: false,
    scanAbortController: null,
    scanID: "",
    scanning: false,
    selected: new Set(),
    token: "",
    toastTimer: 0,
  };

  function readSessionToken() {
    const fragment = new URLSearchParams(window.location.hash.slice(1));
    const fragmentToken = fragment.get("token");
    if (fragmentToken) {
      try {
        window.sessionStorage.setItem("mac-cleanup-studio-token", fragmentToken);
      } catch (_) {
        // A session-only in-memory token still works when storage is disabled.
      }
      window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
      return fragmentToken;
    }
    try {
      return window.sessionStorage.getItem("mac-cleanup-studio-token") || "";
    } catch (_) {
      return "";
    }
  }

  function clearSessionToken() {
    state.token = "";
    try {
      window.sessionStorage.removeItem("mac-cleanup-studio-token");
    } catch (_) {
      // Ignore unavailable storage.
    }
  }

  async function api(path, options = {}) {
    if (!state.token) {
      throw new Error("The local session token is missing.");
    }
    const headers = new Headers(options.headers || {});
    headers.set("Authorization", `Bearer ${state.token}`);
    const response = await window.fetch(path, { ...options, headers });
    if (response.status === 401) {
      clearSessionToken();
      showLockedState();
      throw new Error("The local session expired. Relaunch the dashboard.");
    }
    return response;
  }

  async function responseError(response) {
    try {
      const payload = await response.json();
      return payload.error || `Request failed (${response.status}).`;
    } catch (_) {
      return `Request failed (${response.status}).`;
    }
  }

  async function readNDJSON(response, onEvent) {
    if (!response.ok) {
      throw new Error(await responseError(response));
    }
    if (!response.body) {
      throw new Error("Streaming is unavailable in this browser.");
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    while (true) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
      const lines = buffer.split("\n");
      buffer = lines.pop() || "";
      for (const line of lines) {
        if (line.trim()) {
          onEvent(JSON.parse(line));
        }
      }
      if (done) {
        if (buffer.trim()) {
          onEvent(JSON.parse(buffer));
        }
        break;
      }
    }
  }

  async function loadDisk() {
    if (!state.token) {
      showLockedState();
      return;
    }
    elements.refreshDisk.disabled = true;
    try {
      const response = await api("/api/disk");
      if (!response.ok) {
        throw new Error(await responseError(response));
      }
      const disk = await response.json();
      renderDisk(disk);
      setConnection("Protected · Ready", "ready");
      elements.authNotice.hidden = true;
    } catch (error) {
      setConnection("Connection issue", "error");
      showToast(error.message);
    } finally {
      elements.refreshDisk.disabled = false;
    }
  }

  function renderDisk(disk) {
    const total = nonNegativeNumber(disk.total_bytes);
    const free = Math.min(nonNegativeNumber(disk.free_bytes), total || Number.MAX_SAFE_INTEGER);
    const reportedUsed = nonNegativeNumber(disk.used_bytes);
    const used = total ? Math.min(reportedUsed || total - free, total) : reportedUsed;
    const percent = total ? Math.max(0, Math.min(100, (used / total) * 100)) : 0;
    const volume = typeof disk.volume === "string" && disk.volume.trim() ? disk.volume.trim() : "Local volume";

    elements.diskRingUsage.setAttribute("stroke-dasharray", `${percent} ${100 - percent}`);
    elements.capacityFill.value = percent;
    elements.capacityFill.textContent = `${Math.round(percent)}%`;
    elements.diskRing.setAttribute("aria-label", `${formatBytes(used)} used and ${formatBytes(free)} available on ${volume}`);
    elements.ringFree.textContent = formatBytes(free);
    elements.diskFree.textContent = formatBytes(free);
    elements.diskUsed.textContent = formatBytes(used);
    elements.diskTotal.textContent = formatBytes(total);
    elements.usedPercent.textContent = `${Math.round(percent)}%`;
    elements.volumeLabel.textContent = volume;
    elements.volumeLabel.title = volume;
  }

  async function startScan() {
    if (state.scanning) {
      state.scanAbortController?.abort();
      return;
    }
    if (state.cleaning || !state.token) {
      return;
    }

    resetScanResults();
    state.scanning = true;
    state.scanAbortController = new AbortController();
    elements.scanProgressPanel.hidden = false;
    elements.scanButton.querySelector("span:last-child").textContent = "Stop scan";
    elements.scanProgressTitle.textContent = "Scanning safely…";
    elements.scanProgressDetail.textContent = "Preparing server-owned rules";
    setScanProgress(0);
    setProfileDisabled(true);

    const profile = document.querySelector('input[name="profile"]:checked')?.value || "safe";
    try {
      const response = await api("/api/scan", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ profile }),
        signal: state.scanAbortController.signal,
      });
      await readNDJSON(response, handleScanEvent);
      if (!state.scanID) {
        throw new Error("The scan ended without a reusable scan ID.");
      }
    } catch (error) {
      if (error.name === "AbortError") {
        addScanIssue({ message: "Scan cancelled. Partial results cannot be cleaned; run a new scan when ready." });
        state.scanID = "";
        showToast("Scan cancelled.");
      } else {
        addScanIssue({ message: error.message || "The scan could not finish." });
        state.scanID = "";
        showToast(error.message || "The scan could not finish.");
      }
    } finally {
      state.scanning = false;
      state.scanAbortController = null;
      elements.scanButton.querySelector("span:last-child").textContent = "Scan this Mac";
      elements.scanProgressTitle.textContent = state.scanID ? "Scan complete" : "Scan stopped";
      setProfileDisabled(false);
      updateSelection();
    }
  }

  function handleScanEvent(event) {
    switch (event.type) {
      case "scan.started":
        if (isIdentifier(event.scan_id)) {
          state.scanID = event.scan_id;
        }
        elements.scanProgressDetail.textContent = event.message || "Inspecting cleanup categories";
        break;
      case "scan.progress":
        setScanProgress(normalizeProgress(event.progress));
        elements.scanProgressDetail.textContent = progressDetail(event);
        if (event.reclaimable_bytes !== undefined) {
          elements.reclaimableTotal.textContent = formatBytes(event.reclaimable_bytes);
          elements.reclaimableCaption.textContent = "reclaimable so far";
        }
        break;
      case "scan.category":
        if (event.category && isIdentifier(event.category.rule_id)) {
          addCategory(event.category);
        }
        setScanProgress(normalizeProgress(event.progress));
        elements.scanProgressDetail.textContent = progressDetail(event);
        break;
      case "scan.issue":
        addScanIssue(event.issue || { message: event.message || "A scan rule could not finish." });
        break;
      case "scan.complete":
        completeScan(event.summary || event);
        break;
      default:
        break;
    }
  }

  function resetScanResults() {
    state.categories.clear();
    state.selected.clear();
    state.scanID = "";
    elements.categoryGrid.replaceChildren();
    elements.scanIssues.replaceChildren();
    elements.emptyState.hidden = false;
    elements.scanProgressPanel.hidden = false;
    elements.reclaimableTotal.textContent = "—";
    elements.reclaimableCaption.textContent = "Scanning server-owned rules";
    elements.resultSummary.textContent = "Scanning…";
    elements.confirmationInput.value = "";
    updateSelection();
  }

  function addCategory(category) {
    const normalizedRisk = ["low", "medium", "high"].includes(category.risk) ? category.risk : "high";
    const normalized = {
      ...category,
      risk: normalizedRisk,
      action: category.action === "clean" ? "clean" : "scan_only",
      detected_bytes: nonNegativeNumber(category.detected_bytes),
      reclaimable_bytes: nonNegativeNumber(category.reclaimable_bytes),
      item_count: nonNegativeNumber(category.item_count),
    };
    const actionClean = normalized.action === "clean";
    const cleanable = actionClean && normalized.reclaimable_bytes > 0;
    const scanOnly = !actionClean;
    const existing = elements.categoryGrid.querySelector(`[data-rule-id="${window.CSS.escape(normalized.rule_id)}"]`);
    if (existing) {
      existing.remove();
      state.selected.delete(normalized.rule_id);
    }
    state.categories.set(normalized.rule_id, normalized);

    const fragment = elements.categoryTemplate.content.cloneNode(true);
    const card = fragment.querySelector(".category-card");
    const checkbox = fragment.querySelector(".rule-checkbox");
    const title = fragment.querySelector(".category-title strong");
    const size = fragment.querySelector(".category-title small");
    const riskBadge = fragment.querySelector(".risk-badge");
    const details = fragment.querySelector(".largest-items");
    const list = details.querySelector("ul");

    card.dataset.ruleId = normalized.rule_id;
    card.classList.toggle("is-review-only", scanOnly);
    card.classList.toggle("is-ineligible", actionClean && !cleanable);
    checkbox.value = normalized.rule_id;
    checkbox.id = `rule-${state.categories.size}-${normalized.rule_id.replace(/[^a-zA-Z0-9_-]/g, "-")}`;
    checkbox.disabled = !cleanable;
    checkbox.checked = cleanable && Boolean(normalized.selected_by_default) && normalized.risk === "low";
    checkbox.setAttribute("aria-label", cleanable
      ? `Select ${safeString(normalized.name, "cleanup category")}, ${formatBytes(normalized.reclaimable_bytes)} estimated reclaimable`
      : scanOnly
        ? `${safeString(normalized.name, "cleanup category")}, review only, ${formatBytes(normalized.detected_bytes)} detected`
        : `${safeString(normalized.name, "cleanup category")}, nothing passes the age rule, ${formatBytes(normalized.detected_bytes)} detected`);
    title.textContent = safeString(normalized.name, normalized.rule_id);
    size.textContent = cleanable
      ? `${formatBytes(normalized.reclaimable_bytes)} estimated reclaimable`
      : scanOnly
        ? `${formatBytes(normalized.detected_bytes)} detected`
        : `${formatBytes(normalized.detected_bytes)} detected · 0 eligible`;
    fragment.querySelector(".category-description").textContent = safeString(normalized.description, "No description provided.");
    fragment.querySelector(".fact-age").textContent = safeString(normalized.age, "Rule-defined");
    fragment.querySelector(".fact-action").textContent = cleanable
      ? "Eligible cleanup"
      : scanOnly
        ? "Review only"
        : "Nothing passes age rule";
    fragment.querySelector(".fact-items").textContent = formatCount(normalized.item_count);
    riskBadge.textContent = cleanable ? `${normalized.risk} risk` : scanOnly ? "review only" : "not eligible";
    riskBadge.classList.add(cleanable ? normalized.risk : scanOnly ? "review" : "ineligible");

    const candidates = Array.isArray(normalized.largest_items) ? normalized.largest_items : [];
    details.hidden = candidates.length === 0;
    for (const item of candidates) {
      const row = document.createElement("li");
      const copy = document.createElement("span");
      const itemName = displayString(item.name, "Unnamed item");
      const name = document.createTextNode(itemName);
      copy.append(name);
      const metadata = [displayString(item.location, ""), safeString(item.modified_age, "")].filter(Boolean).join(" · ");
      copy.title = [itemName, metadata].filter(Boolean).join(" · ");
      if (metadata) {
        const small = document.createElement("small");
        small.textContent = metadata;
        copy.append(small);
      }
      const itemSize = document.createElement("strong");
      itemSize.textContent = formatBytes(item.size_bytes);
      row.append(copy, itemSize);
      list.append(row);
    }

    checkbox.addEventListener("change", () => {
      resetConfirmation();
      updateSelection();
    });
    elements.categoryGrid.append(fragment);
    elements.emptyState.hidden = true;
    elements.selectSafe.disabled = false;
    elements.selectNone.disabled = false;
    elements.navRuleCount.textContent = String(state.categories.size);
    updateSelection();
  }

  function addScanIssue(issue) {
    const row = document.createElement("div");
    row.className = "scan-issue";
    const rulePrefix = isIdentifier(issue.rule_id) ? `${issue.rule_id}: ` : "";
    row.textContent = `${rulePrefix}${safeString(issue.message, "A rule could not be scanned completely.")}`;
    elements.scanIssues.append(row);
  }

  function completeScan(summary) {
    if (isIdentifier(summary.scan_id)) {
      state.scanID = summary.scan_id;
    }
    const reclaimable = nonNegativeNumber(summary.reclaimable_bytes) || sumCategories();
    setScanProgress(100);
    elements.scanProgressDetail.textContent = `${formatCount(summary.files_scanned)} inspected · ${formatDuration(summary.duration_millis)}`;
    elements.reclaimableTotal.textContent = formatBytes(reclaimable);
    elements.reclaimableCaption.textContent = `${formatCount(summary.files_scanned)} items inspected`;
    elements.resultSummary.textContent = `${state.categories.size} ${pluralize("rule", state.categories.size)} · ${formatBytes(reclaimable)} reclaimable`;
    elements.opportunityNote.textContent = "Review each selected rule below. The final result is measured from disk space before and after cleanup.";
  }

  function updateSelection() {
    state.selected.clear();
    let bytes = 0;
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox")) {
      const card = checkbox.closest(".category-card");
      card?.classList.toggle("is-selected", checkbox.checked);
      if (checkbox.checked && state.categories.has(checkbox.value)) {
        state.selected.add(checkbox.value);
        bytes += state.categories.get(checkbox.value).reclaimable_bytes;
      }
    }
    const count = state.selected.size;
    elements.selectedTotal.textContent = formatBytes(bytes);
    elements.selectedCount.textContent = String(count);
    elements.dockTotal.textContent = formatBytes(bytes);
    elements.dockRules.textContent = `${count} ${pluralize("rule", count)}`;
    elements.cleanupDock.hidden = count === 0 || !state.scanID || state.cleaning;
    updateCleanButton();
  }

  async function startClean() {
    if (state.cleaning || !state.scanID || !state.selected.size || elements.confirmationInput.value !== "DELETE") {
      return;
    }
    const selectedRules = [...state.selected];
    state.cleaning = true;
    elements.cleanupDock.hidden = true;
    elements.activity.hidden = false;
    elements.activityLog.replaceChildren();
    elements.activityTotal.textContent = "0 B removed";
    elements.cleanProgressTitle.textContent = "Cleaning selected rules…";
    elements.cleanProgressDetail.textContent = "Validating the completed scan";
    setCleanProgress(0);
    elements.scanButton.disabled = true;
    setProfileDisabled(true);
    elements.activity.scrollIntoView({ behavior: "smooth", block: "start" });

    try {
      const response = await api("/api/clean", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          scan_id: state.scanID,
          rule_ids: selectedRules,
          confirmation: "DELETE",
        }),
      });
      await readNDJSON(response, handleCleanEvent);
    } catch (error) {
      addActivity(error.message || "Cleanup could not finish.", "", true);
      showToast(error.message || "Cleanup could not finish.");
    } finally {
      state.cleaning = false;
      elements.scanButton.disabled = false;
      setProfileDisabled(false);
      resetConfirmation();
      updateSelection();
    }
  }

  function handleCleanEvent(event) {
    switch (event.type) {
      case "clean.started":
        elements.cleanProgressDetail.textContent = event.message || "Beginning cleanup";
        addActivity(event.message || "Cleanup started", "");
        break;
      case "clean.progress": {
        const progress = normalizeProgress(event.progress);
        setCleanProgress(progress);
        elements.cleanProgressDetail.textContent = event.message || (event.rule_id ? `Cleaning ${event.rule_id}` : "Cleaning selected rules");
        const removed = nonNegativeNumber(event.removed_bytes);
        elements.activityTotal.textContent = `${formatBytes(removed)} removed`;
        if (event.message || event.rule_id) {
          addActivity(event.message || event.rule_id, formatBytes(removed));
        }
        break;
      }
      case "clean.issue":
        addActivity(event.issue?.message || event.message || "An item could not be cleaned.", event.issue?.rule_id || event.rule_id || "", true);
        break;
      case "clean.partial":
        reportPartialClean(event.summary || event, event);
        break;
      case "clean.complete":
        completeClean(event.summary || event);
        break;
      default:
        break;
    }
  }

  function reportPartialClean(summary, event) {
    setCleanProgress(normalizeProgress(event.progress));
    const removedItems = nonNegativeNumber(summary.removed_items);
    const removedBytes = nonNegativeNumber(summary.removed_bytes);
    const measured = nonNegativeNumber(summary.measured_reclaimed_bytes);
    elements.cleanProgressTitle.textContent = "Cleanup stopped";
    elements.cleanProgressDetail.textContent = `${formatCount(removedItems)} ${pluralize("candidate", removedItems)} removed before it stopped`;
    elements.activityTotal.textContent = `${formatBytes(measured)} measured reclaimed`;
    addActivity(event.message || "Cleanup stopped after making partial progress.", `${formatBytes(removedBytes)} removed`, true);

    state.scanID = "";
    state.selected.clear();
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox")) {
      checkbox.checked = false;
      checkbox.disabled = true;
    }
    elements.resultSummary.textContent = "Partial cleanup · scan again before another cleanup";
    showToast("Cleanup stopped. Partial results are shown in Activity.");
    void loadDisk();
  }

  function completeClean(summary) {
    setCleanProgress(100);
    const measured = nonNegativeNumber(summary.measured_reclaimed_bytes);
    const removed = nonNegativeNumber(summary.removed_bytes);
    elements.cleanProgressTitle.textContent = "Cleanup complete";
    elements.cleanProgressDetail.textContent = `${formatCount(summary.removed_items)} removed · ${formatDuration(summary.duration_millis)}`;
    elements.activityTotal.textContent = `${formatBytes(measured)} measured reclaimed`;
    addActivity("Measured disk space after cleanup", formatBytes(measured));

    elements.measuredTotal.textContent = formatBytes(measured);
    elements.outcomeSelected.textContent = formatBytes(summary.selected_bytes);
    elements.outcomeItems.textContent = formatCount(summary.removed_items);
    elements.outcomeDuration.textContent = formatDuration(summary.duration_millis);
    const delta = measured - removed;
    const variance = removed ? Math.abs(delta) / removed : measured ? 1 : 0;
    elements.outcomeNote.textContent = variance > 0.05
      ? `The measured result differs from summed item sizes by ${formatBytes(Math.abs(delta))}. macOS can reclaim or allocate space while cleanup runs.`
      : "The measured disk-space result closely matches the summed sizes of removed items.";
    if (nonNegativeNumber(summary.failed_items) > 0) {
      elements.outcomeNote.textContent += ` ${formatCount(summary.failed_items)} could not be removed and were left in place.`;
    }

    state.scanID = "";
    state.selected.clear();
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox")) {
      checkbox.checked = false;
      checkbox.disabled = true;
    }
    elements.resultSummary.textContent = "Results are now historical · scan again to refresh";
    if (typeof elements.outcomeDialog.showModal === "function") {
      elements.outcomeDialog.showModal();
    } else {
      elements.outcomeDialog.setAttribute("open", "");
    }
    void loadDisk();
  }

  function addActivity(message, value, isError = false) {
    const item = document.createElement("li");
    item.classList.toggle("is-error", isError);
    const copy = document.createElement("span");
    copy.textContent = safeString(message, isError ? "Cleanup issue" : "Cleanup update");
    const total = document.createElement("strong");
    total.textContent = safeString(value, "");
    item.append(copy, total);
    elements.activityLog.append(item);
    while (elements.activityLog.children.length > 80) {
      elements.activityLog.firstElementChild?.remove();
    }
    item.scrollIntoView({ block: "nearest" });
  }

  function setScanProgress(value) {
    const percent = clampPercent(value);
    elements.scanProgress.value = percent;
    elements.scanProgress.textContent = `${Math.round(percent)}%`;
    elements.scanProgressPercent.textContent = `${Math.round(percent)}%`;
  }

  function setCleanProgress(value) {
    const percent = clampPercent(value);
    elements.cleanProgress.value = percent;
    elements.cleanProgress.textContent = `${Math.round(percent)}%`;
    elements.cleanProgressPercent.textContent = `${Math.round(percent)}%`;
  }

  function setProfileDisabled(disabled) {
    for (const input of elements.profileOptions.querySelectorAll("input")) {
      input.disabled = disabled;
    }
  }

  function resetConfirmation() {
    elements.confirmationInput.value = "";
    elements.confirmationInput.classList.remove("is-valid");
    updateCleanButton();
  }

  function updateCleanButton() {
    const valid = elements.confirmationInput.value === "DELETE";
    elements.confirmationInput.classList.toggle("is-valid", valid);
    elements.cleanButton.disabled = !valid || !state.scanID || state.selected.size === 0 || state.cleaning;
  }

  function setConnection(label, kind) {
    elements.connectionState.querySelector("span:last-child").textContent = label;
    elements.connectionState.classList.toggle("is-ready", kind === "ready");
    elements.connectionState.classList.toggle("is-error", kind === "error");
  }

  function showLockedState() {
    elements.authNotice.hidden = false;
    elements.scanButton.disabled = true;
    elements.refreshDisk.disabled = true;
    setConnection("Session locked", "error");
  }

  function showToast(message) {
    window.clearTimeout(state.toastTimer);
    elements.toast.textContent = safeString(message, "Something went wrong.");
    elements.toast.hidden = false;
    state.toastTimer = window.setTimeout(() => {
      elements.toast.hidden = true;
    }, 5000);
  }

  function progressDetail(event) {
    if (event.message) {
      return event.message;
    }
    const files = nonNegativeNumber(event.files_scanned);
    const bytes = nonNegativeNumber(event.bytes_scanned);
    if (files || bytes) {
      return `${formatCount(files)} inspected · ${formatBytes(bytes)} read`;
    }
    return "Inspecting cleanup categories";
  }

  function sumCategories() {
    let total = 0;
    for (const category of state.categories.values()) {
      total += category.reclaimable_bytes;
    }
    return total;
  }

  function formatBytes(value) {
    let bytes = nonNegativeNumber(value);
    if (bytes < 1024) {
      return `${Math.round(bytes)} B`;
    }
    const units = ["KiB", "MiB", "GiB", "TiB", "PiB"];
    let unitIndex = -1;
    do {
      bytes /= 1024;
      unitIndex += 1;
    } while (bytes >= 1024 && unitIndex < units.length - 1);
    const digits = bytes >= 100 ? 0 : bytes >= 10 ? 1 : 2;
    const display = digits ? bytes.toFixed(digits).replace(/\.?0+$/, "") : bytes.toFixed(0);
    return `${display} ${units[unitIndex]}`;
  }

  function formatCount(value) {
    return new Intl.NumberFormat().format(Math.round(nonNegativeNumber(value)));
  }

  function formatDuration(milliseconds) {
    const value = nonNegativeNumber(milliseconds);
    if (value < 1000) {
      return `${Math.round(value)}ms`;
    }
    if (value < 60000) {
      return `${(value / 1000).toFixed(value < 10000 ? 1 : 0)}s`;
    }
    const minutes = Math.floor(value / 60000);
    const seconds = Math.round((value % 60000) / 1000);
    return `${minutes}m ${seconds}s`;
  }

  function nonNegativeNumber(value) {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : 0;
  }

  function safeString(value, fallback) {
    return typeof value === "string" && value.trim() ? value.trim().slice(0, 500) : fallback;
  }

  function displayString(value, fallback) {
    return typeof value === "string" && value ? value : fallback;
  }

  function isIdentifier(value) {
    return typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(value);
  }

  function normalizeProgress(value) {
    const number = nonNegativeNumber(value);
    return number > 0 && number <= 1 ? number * 100 : number;
  }

  function clampPercent(value) {
    return Math.max(0, Math.min(100, Number(value) || 0));
  }

  function pluralize(word, count) {
    return count === 1 ? word : `${word}s`;
  }

  elements.profileOptions.addEventListener("change", (event) => {
    if (!event.target.matches('input[name="profile"]')) {
      return;
    }
    for (const card of elements.profileOptions.querySelectorAll(".profile-card")) {
      card.classList.toggle("is-selected", card.contains(event.target));
    }
  });

  elements.scanButton.addEventListener("click", startScan);
  elements.cleanButton.addEventListener("click", startClean);
  elements.refreshDisk.addEventListener("click", loadDisk);
  elements.confirmationInput.addEventListener("input", updateCleanButton);
  elements.confirmationInput.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !elements.cleanButton.disabled) {
      event.preventDefault();
      void startClean();
    }
  });
  elements.selectNone.addEventListener("click", () => {
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox")) {
      checkbox.checked = false;
    }
    resetConfirmation();
    updateSelection();
  });
  elements.selectSafe.addEventListener("click", () => {
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox")) {
      const category = state.categories.get(checkbox.value);
      if (!checkbox.disabled && category?.risk === "low") {
        checkbox.checked = true;
      }
    }
    resetConfirmation();
    updateSelection();
  });

  const hour = new Date().getHours();
  elements.dayPeriod.textContent = hour < 12 ? "morning" : hour < 18 ? "afternoon" : "evening";
  state.token = readSessionToken();
  if (state.token) {
    void loadDisk();
  } else {
    showLockedState();
  }
})();
