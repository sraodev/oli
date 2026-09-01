(() => {
  "use strict";

  const elements = {
    selectionStatus: document.querySelector("#selection-status"),
    atlasButton: document.querySelector("#atlas-button"),
    atlasScope: document.querySelector("#atlas-scope"),
    atlasStatus: document.querySelector("#atlas-status"),
    atlasResults: document.querySelector("#atlas-results"),
    atlasProgress: document.querySelector("#atlas-progress"),
    atlasAllocated: document.querySelector("#atlas-allocated"),
    atlasLogical: document.querySelector("#atlas-logical"),
    atlasCount: document.querySelector("#atlas-count"),
    atlasScopes: document.querySelector("#atlas-scopes"),
    atlasItems: document.querySelector("#atlas-items"),
    atlasListNote: document.querySelector("#atlas-list-note"),
    atlasWarnings: document.querySelector("#atlas-warnings"),
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
    exploring: false,
    exploreAbortController: null,
    exploration: null,
    atlasList: "folders",
    categories: new Map(),
    cleaning: false,
    scanAbortController: null,
    scanID: "",
    scanning: false,
    selected: new Set(),
    selectionReady: false,
    selectionVersion: 0,
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
      setConnection("Local · Ready", "ready");
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
    if (state.cleaning || state.exploring || !state.token) {
      return;
    }

    resetScanResults();
    state.scanning = true;
    elements.atlasButton.disabled = true;
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
      elements.atlasButton.disabled = !state.token;
      state.scanAbortController = null;
      elements.scanButton.querySelector("span:last-child").textContent = "Scan this Mac";
      if (!state.scanID) elements.scanProgressTitle.textContent = "Scan stopped";
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
    const cleanable = actionClean && (normalized.largest_items || []).some(item => item.eligible && isIdentifier(item.candidate_id));
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
      const pick = document.createElement("input");
      pick.type = "checkbox";
      pick.className = "candidate-checkbox";
      pick.value = item.candidate_id || "";
      pick.disabled = !actionClean || !item.eligible || !isIdentifier(item.candidate_id);
      pick.checked = !pick.disabled && checkbox.checked;
      pick.setAttribute("aria-label", `Select ${displayString(item.name, "candidate")}`);
      pick.addEventListener("change", () => { resetConfirmation(); updateSelection(); });
      const itemName = displayString(item.name, "Unnamed item");
      const name = document.createTextNode(itemName);
      copy.append(name);
      const metadata = [safeString(item.modified_age, ""), `${normalized.risk} risk`, item.eligible ? "Eligible" : "Not eligible", safeString(item.reason, "")].filter(Boolean).join(" · ");
      copy.title = [itemName, metadata].filter(Boolean).join(" · ");
      if (metadata) {
        const small = document.createElement("small");
        small.textContent = metadata;
        copy.append(small);
      }
      const itemSize = document.createElement("strong");
      itemSize.textContent = formatBytes(item.size_bytes);
      row.append(pick, copy, itemSize);
      list.append(row);
    }

    checkbox.addEventListener("change", () => {
      for (const item of card.querySelectorAll(".candidate-checkbox:not(:disabled)")) { item.checked = checkbox.checked; }
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
    elements.scanProgressTitle.textContent = summary.partial ? "Scan finished · incomplete coverage" : "Scan complete";
    if (isIdentifier(summary.scan_id)) {
      state.scanID = summary.scan_id;
    }
    const reclaimable = nonNegativeNumber(summary.reclaimable_bytes);
    setScanProgress(100);
    elements.scanProgressDetail.textContent = `${formatCount(summary.files_scanned)} inspected · ${formatDuration(summary.duration_millis)}`;
    elements.reclaimableTotal.textContent = formatBytes(reclaimable);
    elements.reclaimableCaption.textContent = `${formatCount(summary.files_scanned)} items inspected`;
    elements.resultSummary.textContent = `${summary.partial ? "Incomplete coverage · " : ""}${state.categories.size} ${pluralize("rule", state.categories.size)} · ${formatBytes(reclaimable)} estimated`;
    elements.opportunityNote.textContent = "Review individual candidates below. A directory includes its scanned contents; final disk-space change is measured after cleanup.";
    if (summary.partial) {
      elements.opportunityNote.textContent = `Some locations were not fully inspected. These totals cover only measured items, not all disk usage. ${nonNegativeNumber(summary.warnings_omitted)} additional warnings omitted.`;
    }
  }

  async function updateSelection() {
    const version = ++state.selectionVersion;
    state.selectionReady = false;
    state.selected.clear();
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox")) {
      const card = checkbox.closest(".category-card");
      const items = [...card.querySelectorAll(".candidate-checkbox:not(:disabled)")];
      const checked = items.filter(item => item.checked);
      checkbox.checked = items.length > 0 && checked.length === items.length;
      checkbox.indeterminate = checked.length > 0 && checked.length < items.length;
      card.classList.toggle("is-selected", checked.length > 0);
      for (const item of checked) state.selected.add(item.value);
    }
    const count = state.selected.size;
    elements.selectedTotal.textContent = count ? "…" : "0 B";
    elements.selectedCount.textContent = String(count);
    elements.dockTotal.textContent = count ? "…" : "0 B";
    elements.dockRules.textContent = `${count} ${pluralize("candidate", count)}`;
    elements.cleanupDock.hidden = count === 0 || !state.scanID || state.cleaning;
    elements.selectionStatus.textContent = "";
    updateCleanButton();
    if (!count || !state.scanID || state.scanning || state.cleaning) return;
    if (count > 128) { elements.selectionStatus.textContent = "Select at most 128 candidates per cleanup. Nothing has been deleted."; return; }
    elements.selectionStatus.textContent = "Checking selection totals against the current scan…";
    try {
      const response = await api("/api/selection", { method: "POST", headers: {"Content-Type":"application/json"}, body: JSON.stringify({scan_id: state.scanID, candidate_ids: [...state.selected]}) });
      if (!response.ok) throw new Error(await responseError(response));
      const selection = await response.json();
      if (version !== state.selectionVersion) return;
      if (selection.scan_id !== state.scanID || selection.candidate_ids.length !== count || !selection.candidate_ids.every(id => state.selected.has(id))) throw new Error("Selection changed. Scan again before cleaning.");
      elements.selectedTotal.textContent = formatBytes(selection.metrics.allocated_bytes);
      elements.dockTotal.textContent = formatBytes(selection.metrics.allocated_bytes);
      elements.selectionStatus.textContent = `${count} selected · ${formatBytes(selection.metrics.allocated_bytes)} allocated estimate · ${formatBytes(selection.metrics.logical_bytes)} logical. Shared hard-linked data counts once.`;
      state.selectionReady = true;
    } catch (error) {
      if (version === state.selectionVersion) elements.selectionStatus.textContent = error.message || "Selection could not be verified. Review or rescan.";
    }
    if (version === state.selectionVersion) updateCleanButton();
  }

  async function startClean() {
    if (state.cleaning || state.exploring || state.scanning || !state.selectionReady || !state.scanID || !state.selected.size || elements.confirmationInput.value !== "DELETE") {
      return;
    }
    const selectedCandidates = [...state.selected];
    state.cleaning = true;
    for (const item of elements.categoryGrid.querySelectorAll(".rule-checkbox, .candidate-checkbox")) item.disabled = true;
    elements.atlasButton.disabled = true;
    elements.cleanupDock.hidden = true;
    elements.activity.hidden = false;
    elements.activityLog.replaceChildren();
    elements.activityTotal.textContent = "0 B removed";
    elements.cleanProgressTitle.textContent = "Cleaning selected candidates…";
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
          candidate_ids: selectedCandidates,
          confirmation: "DELETE",
        }),
      });
      await readNDJSON(response, handleCleanEvent);
    } catch (error) {
      addActivity(error.message || "Cleanup could not finish.", "", true);
      showToast(error.message || "Cleanup could not finish.");
    } finally {
      state.cleaning = false;
      state.scanID = "";
      for (const item of elements.categoryGrid.querySelectorAll(".rule-checkbox, .candidate-checkbox")) { item.checked = false; item.disabled = true; }
      elements.resultSummary.textContent = "Results are now historical · scan again to refresh";
      elements.atlasButton.disabled = !state.token;
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
        elements.cleanProgressDetail.textContent = event.message || (event.rule_id ? `Cleaning ${event.rule_id}` : "Cleaning selected candidates");
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
    const observed = observationText(summary);
    elements.cleanProgressTitle.textContent = "Cleanup stopped";
    elements.cleanProgressDetail.textContent = `${formatCount(removedItems)} ${pluralize("candidate", removedItems)} removed before it stopped`;
    elements.activityTotal.textContent = observed;
    addActivity(event.message || "Cleanup stopped after making partial progress.", `${formatBytes(removedBytes)} removed`, true);

    state.scanID = "";
    state.selected.clear();
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox, .candidate-checkbox")) {
      checkbox.checked = false;
      checkbox.disabled = true;
    }
    elements.resultSummary.textContent = "Partial cleanup · scan again before another cleanup";
    showToast("Cleanup stopped. Partial results are shown in Activity.");
    void loadDisk();
  }

  function completeClean(summary) {
    setCleanProgress(100);
    const measured = Number(summary.observed_free_space_change_bytes) || 0;
    const observed = observationText(summary);
    const removed = nonNegativeNumber(summary.removed_bytes);
    elements.cleanProgressTitle.textContent = summary.partial ? "Cleanup finished with issues" : "Cleanup complete";
    elements.cleanProgressDetail.textContent = `${formatCount(summary.removed_items)} removed · ${formatDuration(summary.duration_millis)}`;
    elements.activityTotal.textContent = observed;
    addActivity("Observed disk space after cleanup", observed);

    elements.measuredTotal.textContent = summary.observation_available === false ? "Unavailable" : `${measured < 0 ? "−" : "+"}${formatBytes(Math.abs(measured))}`;
    elements.outcomeSelected.textContent = formatBytes(summary.selected_bytes);
    elements.outcomeItems.textContent = formatCount(summary.removed_items);
    elements.outcomeDuration.textContent = formatDuration(summary.duration_millis);
    const delta = measured - removed;
    const variance = removed ? Math.abs(delta) / removed : measured ? 1 : 0;
    elements.outcomeNote.textContent = variance > 0.05
      ? `The measured result differs from summed item sizes by ${formatBytes(Math.abs(delta))}. macOS can reclaim or allocate space while cleanup runs.`
      : "The measured disk-space result closely matches the summed sizes of removed items.";
    if (nonNegativeNumber(summary.failed_items) > 0) {
      elements.outcomeNote.textContent += ` ${formatCount(summary.failed_items)} candidates could not be fully removed; some contents may already be gone.`;
    }

    state.scanID = "";
    state.selected.clear();
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox, .candidate-checkbox")) {
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

  function observationText(summary) {
    if (summary.observation_available === false) return "Free-space observation unavailable";
    const delta = Number(summary.observed_free_space_change_bytes) || 0;
    return `${delta < 0 ? "−" : "+"}${formatBytes(Math.abs(delta))} observed free-space change`;
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
    elements.cleanButton.disabled = !valid || !state.selectionReady || !state.scanID || state.selected.size === 0 || state.cleaning || state.scanning || state.exploring;
  }

  function setConnection(label, kind) {
    elements.connectionState.querySelector("span:last-child").textContent = label;
    elements.connectionState.classList.toggle("is-ready", kind === "ready");
    elements.connectionState.classList.toggle("is-error", kind === "error");
  }

  function showLockedState() {
    elements.atlasButton.disabled = true;
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
    for (const checkbox of elements.categoryGrid.querySelectorAll(".candidate-checkbox")) {
      checkbox.checked = false;
    }
    resetConfirmation();
    updateSelection();
  });
  elements.selectSafe.addEventListener("click", () => {
    for (const checkbox of elements.categoryGrid.querySelectorAll(".rule-checkbox")) {
      const category = state.categories.get(checkbox.value);
      if (!checkbox.disabled && category?.risk === "low") {
        for (const item of checkbox.closest(".category-card").querySelectorAll(".candidate-checkbox:not(:disabled)")) { item.checked = true; }
      }
    }
    resetConfirmation();
    updateSelection();
  });

  async function startExplore() {
    if (state.exploring) {
      state.exploreAbortController?.abort();
      return;
    }
    if (state.scanning || state.cleaning || !state.token) return;
    state.exploring = true;
    state.exploreAbortController = new AbortController();
    state.exploration = null;
    elements.atlasResults.hidden = true;
    elements.atlasProgress.hidden = false;
    elements.atlasButton.textContent = "Stop exploring";
    elements.atlasScope.disabled = true;
    elements.scanButton.disabled = true;
    elements.atlasStatus.textContent = "Inspecting metadata… Up to 200,000 entries, with a two-minute time limit.";
    updateCleanButton();
    try {
      const response = await api("/api/explore", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ scope: elements.atlasScope.value }),
        signal: state.exploreAbortController.signal,
      });
      if (!response.ok) throw new Error(await responseError(response));
      state.exploration = await response.json();
      renderExploration();
    } catch (error) {
      elements.atlasStatus.textContent = error.name === "AbortError" ? "Exploration stopped. No files were changed." : (error.message || "Exploration could not finish.");
    } finally {
      state.exploring = false;
      state.exploreAbortController = null;
      elements.atlasProgress.hidden = true;
      elements.atlasButton.textContent = "Explore storage";
      elements.atlasScope.disabled = false;
      elements.scanButton.disabled = !state.token;
      updateCleanButton();
    }
  }

  function renderExploration() {
    const report = state.exploration;
    document.dispatchEvent(new CustomEvent("storage-map:report", {detail: report}));
    elements.atlasResults.hidden = false;
    elements.atlasStatus.textContent = `${report.partial ? "Partial report" : "Inspection complete"} · ${formatCount(report.entries_inspected)} entries · ${formatCount(report.total.symlinks)} symlinks skipped. No files changed.`;
    if (report.warnings_omitted) elements.atlasStatus.textContent += ` ${report.warnings_omitted} additional warnings omitted.`;
    elements.atlasAllocated.textContent = formatBytes(report.total.allocated_bytes);
    elements.atlasLogical.textContent = formatBytes(report.total.logical_bytes);
    elements.atlasCount.textContent = formatCount(report.total.unique_files);
    elements.atlasScopes.replaceChildren();
    for (const scope of report.scopes) {
      const card = document.createElement("div");
      card.className = "atlas-scope-card";
      const name = document.createElement("span");
      name.textContent = scope.display_path;
      const size = document.createElement("strong");
      size.textContent = formatBytes(scope.metrics.allocated_bytes);
      const meter = document.createElement("progress");
      meter.max = Math.max(1, report.total.allocated_bytes);
      meter.value = scope.metrics.allocated_bytes;
      meter.setAttribute("aria-label", `${scope.name}: ${size.textContent} allocated`);
      card.append(name, size, meter);
      elements.atlasScopes.append(card);
    }
    elements.atlasWarnings.replaceChildren();
    for (const warning of report.warnings) {
      const row = document.createElement("div");
      row.className = "scan-issue";
      row.textContent = `${warning.display_path}: ${warning.message}`;
      elements.atlasWarnings.append(row);
    }
    renderAtlasList();
  }

  function renderAtlasList() {
    const report = state.exploration;
    if (!report) return;
    const notes = {
      folders: "Top-level folders, ranked by logical size. A hard-linked file is counted only at its first encountered location.",
      large_files: `Files at least ${formatBytes(report.min_size_bytes)}, ranked by logical size.`,
      old_files: `Files not modified for at least ${report.older_than_days} days, largest first. Age does not mean safe to delete.`,
    };
    elements.atlasListNote.textContent = `${notes[state.atlasList]} Showing up to ${report.limit}.`;
    elements.atlasItems.replaceChildren();
    for (const item of report[state.atlasList]) {
      const row = document.createElement("tr");
      for (const value of [item.display_path, formatBytes(item.metrics.allocated_bytes), formatBytes(item.metrics.logical_bytes), new Date(item.modified).toLocaleDateString()]) {
        const cell = document.createElement("td");
        cell.textContent = value;
        row.append(cell);
      }
      elements.atlasItems.append(row);
    }
    if (!report[state.atlasList].length) {
      const row = document.createElement("tr");
      const cell = document.createElement("td");
      cell.colSpan = 4;
      cell.textContent = "No matching items in this scope. Check any scan warnings below.";
      row.append(cell);
      elements.atlasItems.append(row);
    }
  }

  elements.atlasButton.addEventListener("click", startExplore);
  function updateNavigation() {
    const section = window.location.hash || "#overview";
    for (const link of document.querySelectorAll('.sidebar nav a')) {
      const active = link.getAttribute("href") === section;
      link.classList.toggle("is-active", active);
      if (active) link.setAttribute("aria-current", "location");
      else link.removeAttribute("aria-current");
    }
  }
  window.addEventListener("hashchange", updateNavigation);
  for (const button of document.querySelectorAll("[data-atlas-list]")) {
    button.addEventListener("click", () => {
      state.atlasList = button.dataset.atlasList;
      for (const tab of document.querySelectorAll("[data-atlas-list]")) {
        tab.setAttribute("aria-pressed", String(tab === button));
      }
      renderAtlasList();
    });
  }

  const hour = new Date().getHours();
  elements.dayPeriod.textContent = hour < 12 ? "morning" : hour < 18 ? "afternoon" : "evening";
  state.token = readSessionToken();
  updateNavigation();
  if (state.token) {
    void loadDisk();
  } else {
    showLockedState();
  }
})();
