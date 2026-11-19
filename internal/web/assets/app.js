"use strict";

const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
const svgNS = "http://www.w3.org/2000/svg";

const state = {
  view: "files",
  path: "",
  entries: [],
  searchResults: null,
  searchQuery: "",
  sort: { key: "name", dir: 1 },
  session: null,
  uploads: new Map(),
  uploadQueue: [],
  activeUploads: 0,
  uploadEls: new Map(),
  reloadTimer: null,
  move: { source: null, path: "", busy: false },
  share: { source: null, url: "" },
  menuCleanup: null,
};

const els = {
  search: $("#search"),
  uploadBtn: $("#upload-btn"),
  newFolderBtn: $("#new-folder-btn"),
  trashBtn: $("#trash-btn"),
  sharesBtn: $("#shares-btn"),
  logoutBtn: $("#logout-btn"),
  refreshBtn: $("#refresh-btn"),
  fileInput: $("#file-input"),
  fileList: $("#file-list"),
  emptyState: $("#empty-state"),
  emptyTitle: $("#empty-title"),
  emptyText: $("#empty-text"),
  tableHead: $("#table-head"),
  breadcrumbs: $("#breadcrumbs"),
  viewInfo: $("#view-info"),
  viewAction: $("#view-action"),
  quotaFill: $("#quota-fill"),
  quotaText: $("#quota-text"),
  quotaPill: $("#quota-pill"),
  policy: $("#policy"),
  dropOverlay: $("#drop-overlay"),
  dropTarget: $("#drop-target"),
  uploadTray: $("#upload-tray"),
  uploadList: $("#upload-list"),
  clearUploads: $("#clear-uploads"),
  menuRoot: $("#menu-root"),
  toasts: $("#toasts"),
  dialogInput: $("#dialog-input"),
  inputForm: $("#input-form"),
  inputTitle: $("#input-title"),
  inputDesc: $("#input-desc"),
  inputLabel: $("#input-label"),
  inputValue: $("#input-value"),
  inputSubmit: $("#input-submit"),
  dialogConfirm: $("#dialog-confirm"),
  confirmTitle: $("#confirm-title"),
  confirmMessage: $("#confirm-message"),
  confirmOk: $("#confirm-ok"),
  dialogMove: $("#dialog-move"),
  moveTitle: $("#move-title"),
  moveCrumbs: $("#move-crumbs"),
  moveList: $("#move-list"),
  moveOk: $("#move-ok"),
  dialogShare: $("#dialog-share"),
  shareTitle: $("#share-title"),
  shareExpiryField: $("#share-expiry-field"),
  shareExpiry: $("#share-expiry"),
  shareResult: $("#share-result"),
  shareUrl: $("#share-url"),
  shareCopy: $("#share-copy"),
  shareOpen: $("#share-open"),
  shareCreate: $("#share-create"),
  dialogPreview: $("#dialog-preview"),
  previewTitle: $("#preview-title"),
  previewMeta: $("#preview-meta"),
  previewBody: $("#preview-body"),
  previewDownload: $("#preview-download"),
};

const dateFmt = new Intl.DateTimeFormat(undefined, {
  year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit",
});

init();

async function init() {
  bindEvents();
  try {
    state.session = await api("/api/session");
  } catch {
    return;
  }
  applySession();
  parseLocation();
  await refresh(false);
}

function bindEvents() {
  els.uploadBtn.addEventListener("click", () => els.fileInput.click());
  els.fileInput.addEventListener("change", () => {
    enqueueFiles(els.fileInput.files);
    els.fileInput.value = "";
  });
  els.newFolderBtn.addEventListener("click", () => promptNewFolder());
  els.trashBtn.addEventListener("click", () => setView("trash"));
  els.sharesBtn.addEventListener("click", () => setView("shares"));
  els.logoutBtn.addEventListener("click", logout);
  els.refreshBtn.addEventListener("click", () => refresh(false));
  els.clearUploads.addEventListener("click", clearFinishedUploads);
  els.moveOk.addEventListener("click", moveHere);
  els.shareCreate.addEventListener("click", createShare);
  els.shareCopy.addEventListener("click", () => {
    if (state.share.url) copyText(state.share.url);
  });

  $$(".sort-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      const key = btn.dataset.sort;
      if (state.sort.key === key) {
        state.sort.dir *= -1;
      } else {
        state.sort = { key, dir: 1 };
      }
      renderFiles();
    });
  });

  els.search.addEventListener("input", onSearchInput);

  $$("[data-close]").forEach((btn) => {
    btn.addEventListener("click", () => btn.closest("dialog")?.close());
  });
  $$("dialog").forEach((dialog) => {
    dialog.addEventListener("click", (event) => {
      if (event.target === dialog) dialog.close();
    });
  });
  els.dialogPreview.addEventListener("close", () => {
    els.previewBody.replaceChildren();
  });

  window.addEventListener("popstate", () => {
    parseLocation();
    refresh(false);
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") closeMenu();
  });

  let dragDepth = 0;
  const hasFiles = (event) => Array.from(event.dataTransfer?.types || []).includes("Files");
  document.addEventListener("dragenter", (event) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    dragDepth++;
    if (state.view === "files") {
      els.dropTarget.textContent = state.path ? `Upload to /${state.path}` : "Upload to Files";
      els.dropOverlay.hidden = false;
    }
  });
  document.addEventListener("dragover", (event) => {
    if (hasFiles(event)) event.preventDefault();
  });
  document.addEventListener("dragleave", (event) => {
    if (!hasFiles(event)) return;
    dragDepth = Math.max(0, dragDepth - 1);
    if (dragDepth === 0) els.dropOverlay.hidden = true;
  });
  document.addEventListener("drop", (event) => {
    if (!hasFiles(event)) return;
    event.preventDefault();
    dragDepth = 0;
    els.dropOverlay.hidden = true;
    if (state.view === "files") enqueueFiles(event.dataTransfer.files);
  });
}

async function api(url, options = {}) {
  const opts = { credentials: "same-origin", ...options };
  opts.headers = { "X-Fileshare": "1", ...(options.headers || {}) };
  if (opts.body !== undefined && !(opts.body instanceof FormData)) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(opts.body);
  }
  const res = await fetch(url, opts);
  if (res.status === 401) {
    location.replace("/login");
    throw new Error("unauthenticated");
  }
  const type = res.headers.get("content-type") || "";
  const data = type.includes("application/json") ? await res.json().catch(() => ({})) : await res.text();
  if (!res.ok) {
    const message = data && data.error ? data.error : (typeof data === "string" && data ? data : "Request failed");
    throw new Error(message);
  }
  return data;
}

function applySession() {
  if (!state.session) return;
  const used = Number(state.session.usedBytes || 0);
  const limit = Number(state.session.quotaBytes || 0);
  const pct = limit > 0 ? Math.min(100, (used / limit) * 100) : 0;
  els.quotaFill.style.width = `${pct.toFixed(1)}%`;
  els.quotaText.textContent = `${formatBytes(used)} / ${formatBytes(limit)}`;
  els.quotaPill.classList.toggle("warn", pct >= 85);
  const retention = Number(state.session.retentionDays || 0);
  const trashDays = Number(state.session.trashDays || 0);
  const parts = [];
  parts.push(retention > 0
    ? `Files older than ${retention} days are moved to Trash automatically.`
    : "Automatic file cleanup is off.");
  parts.push(trashDays > 0
    ? `Trash is emptied after ${trashDays} days.`
    : "Trash is emptied manually.");
  if (state.session.webdav) {
    parts.push("WebDAV is available at /dav/.");
  }
  els.policy.textContent = parts.join(" ");
}

async function refreshSession() {
  try {
    state.session = await api("/api/session");
    applySession();
  } catch {
    /* ignore */
  }
}

function parseLocation() {
  const params = new URLSearchParams(location.search);
  const view = params.get("view");
  state.view = view === "trash" || view === "shares" ? view : "files";
  state.path = state.view === "files" ? (params.get("p") || "") : "";
}

function urlFor(view, path) {
  if (view === "trash") return "/?view=trash";
  if (view === "shares") return "/?view=shares";
  const p = new URLSearchParams();
  if (path) p.set("p", path);
  const query = p.toString();
  return query ? `/?${query}` : "/";
}

async function refresh(push) {
  if (state.view === "trash") return loadTrash(push);
  if (state.view === "shares") return loadShares(push);
  return loadFiles(state.path, { push, keepSearch: false });
}

async function setView(view) {
  if (state.view === view) return;
  state.view = view;
  state.searchResults = null;
  state.searchQuery = "";
  els.search.value = "";
  closeMenu();
  if (view === "trash") {
    await loadTrash(true);
  } else if (view === "shares") {
    await loadShares(true);
  } else {
    await loadFiles(state.path || "", { push: true, keepSearch: false });
  }
}

async function loadFiles(path, options = {}) {
  const { push = true, keepSearch = false } = options;
  try {
    const data = await api(`/api/list?${new URLSearchParams({ path })}`);
    state.view = "files";
    state.path = data.path;
    state.entries = data.entries || [];
    if (!keepSearch) {
      state.searchResults = null;
      state.searchQuery = "";
      els.search.value = "";
    }
    const url = urlFor("files", state.path);
    if (push) history.pushState({}, "", url);
    else history.replaceState({}, "", url);
    renderFiles();
  } catch (err) {
    if (path && /not found/i.test(err.message)) {
      return loadFiles("", { push, keepSearch: false });
    }
    toast(err.message, "error");
  }
}

async function loadTrash(push) {
  try {
    const data = await api("/api/trash");
    state.view = "trash";
    state.entries = [];
    state.searchResults = null;
    const url = urlFor("trash");
    if (push) history.pushState({}, "", url);
    else history.replaceState({}, "", url);
    renderTrash(data);
  } catch (err) {
    toast(err.message, "error");
  }
}

async function loadShares(push) {
  try {
    const data = await api("/api/shares");
    state.view = "shares";
    state.entries = [];
    state.searchResults = null;
    const url = urlFor("shares");
    if (push) history.pushState({}, "", url);
    else history.replaceState({}, "", url);
    renderShares(data.items || []);
  } catch (err) {
    toast(err.message, "error");
  }
}

function updateControls() {
  const isFiles = state.view === "files";
  els.uploadBtn.disabled = !isFiles;
  els.newFolderBtn.disabled = !isFiles;
  els.search.disabled = !isFiles;
  els.tableHead.hidden = !isFiles;
  els.trashBtn.classList.toggle("active", state.view === "trash");
  els.sharesBtn.classList.toggle("active", state.view === "shares");
  if (!isFiles) {
    els.search.value = "";
    state.searchResults = null;
    state.searchQuery = "";
  }
}

function renderFiles() {
  updateControls();
  renderFileBreadcrumbs(state.path);
  els.policy.hidden = false;
  els.viewAction.hidden = true;
  els.viewAction.onclick = null;

  const source = state.searchResults ?? state.entries;
  const entries = sortEntries(source);
  els.fileList.replaceChildren();
  for (const entry of entries) els.fileList.append(fileRow(entry));

  const count = entries.length;
  if (count === 0) {
    els.emptyState.hidden = false;
    if (state.searchResults !== null) {
      els.emptyTitle.textContent = "No matches";
      els.emptyText.textContent = `Nothing matched “${state.searchQuery}”.`;
    } else {
      els.emptyTitle.textContent = "This folder is empty";
      els.emptyText.textContent = "Upload files or create a folder to get started.";
    }
  } else {
    els.emptyState.hidden = true;
  }
  els.viewInfo.textContent = state.searchResults !== null
    ? `${count} result${count === 1 ? "" : "s"} for “${state.searchQuery}”`
    : `${count} item${count === 1 ? "" : "s"}`;
  updateSortIndicators();
}

function renderTrash(data) {
  updateControls();
  renderSimpleBreadcrumb("Trash", "i-trash");
  els.policy.hidden = false;
  els.fileList.replaceChildren();
  const items = data.items || [];
  for (const item of items) els.fileList.append(trashRow(item));

  const count = items.length;
  els.viewInfo.textContent = `${count} item${count === 1 ? "" : "s"} · ${formatBytes(data.totalBytes || 0)}`;
  els.viewAction.hidden = count === 0;
  els.viewAction.textContent = "Empty trash";
  els.viewAction.onclick = emptyTrash;
  els.emptyState.hidden = count !== 0;
  if (count === 0) {
    els.emptyTitle.textContent = "Trash is empty";
    els.emptyText.textContent = "Deleted files will appear here before they are permanently removed.";
  }
}

function renderShares(items) {
  updateControls();
  renderSimpleBreadcrumb("Shared links", "i-link");
  els.policy.hidden = false;
  els.fileList.replaceChildren();
  for (const item of items) els.fileList.append(shareRow(item));

  els.viewInfo.textContent = `${items.length} link${items.length === 1 ? "" : "s"}`;
  els.viewAction.hidden = true;
  els.viewAction.onclick = null;
  els.emptyState.hidden = items.length !== 0;
  if (items.length === 0) {
    els.emptyTitle.textContent = "No shared links";
    els.emptyText.textContent = "Create a read-only link from any file or folder's menu.";
  }
}

function renderFileBreadcrumbs(path) {
  els.breadcrumbs.replaceChildren();
  const root = document.createElement("button");
  root.type = "button";
  root.innerHTML = "";
  root.append(icon("i-home", "icon small"));
  root.append(document.createTextNode("Files"));
  if (!path) {
    root.classList.add("current");
  } else {
    root.addEventListener("click", () => loadFiles("", { push: true }));
  }
  els.breadcrumbs.append(root);
  if (!path) return;
  const parts = path.split("/").filter(Boolean);
  let prefix = "";
  parts.forEach((part, index) => {
    const sep = document.createElement("span");
    sep.className = "sep";
    sep.textContent = "/";
    els.breadcrumbs.append(sep);
    prefix = prefix ? `${prefix}/${part}` : part;
    if (index === parts.length - 1) {
      const current = document.createElement("span");
      current.className = "current";
      current.textContent = part;
      els.breadcrumbs.append(current);
    } else {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = part;
      const target = prefix;
      button.addEventListener("click", () => loadFiles(target, { push: true }));
      els.breadcrumbs.append(button);
    }
  });
}

function renderSimpleBreadcrumb(label, iconID) {
  els.breadcrumbs.replaceChildren();
  const root = document.createElement("button");
  root.type = "button";
  root.append(icon("i-home", "icon small"));
  root.append(document.createTextNode("Files"));
  root.addEventListener("click", () => setView("files"));
  els.breadcrumbs.append(root);
  const sep = document.createElement("span");
  sep.className = "sep";
  sep.textContent = "/";
  els.breadcrumbs.append(sep);
  const span = document.createElement("span");
  span.className = "current";
  span.style.display = "inline-flex";
  span.style.alignItems = "center";
  span.style.gap = "6px";
  span.append(icon(iconID, "icon small"));
  span.append(document.createTextNode(label));
  els.breadcrumbs.append(span);
}

function fileRow(entry) {
  const li = document.createElement("li");
  li.className = `file-row${entry.isDir ? " is-dir" : ""}`;
  li.dataset.path = entry.path;

  const main = document.createElement("div");
  main.className = "row-main";
  main.tabIndex = 0;
  main.setAttribute("role", "button");
  main.setAttribute("aria-label", `${entry.isDir ? "Open folder" : "Preview"} ${entry.name}`);
  main.append(iconBox(entry.kind));

  const text = document.createElement("div");
  text.className = "row-text";
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = entry.name;
  name.title = entry.name;
  const sub = document.createElement("span");
  sub.className = "row-sub";
  sub.textContent = `${formatBytes(entry.size)} · ${formatDate(entry.modTime)}`;
  text.append(name, sub);
  main.append(text);

  const open = () => {
    if (entry.isDir) loadFiles(entry.path, { push: true });
    else openPreview(entry);
  };
  main.addEventListener("click", open);
  main.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      open();
    }
  });

  const size = document.createElement("span");
  size.className = "size";
  size.textContent = entry.isDir ? "—" : formatBytes(entry.size);
  const modified = document.createElement("span");
  modified.className = "modified";
  modified.textContent = formatDate(entry.modTime);

  const actions = document.createElement("div");
  actions.className = "row-actions";
  const more = iconButton("i-more", "More actions");
  more.addEventListener("click", (event) => {
    event.stopPropagation();
    openMenu(more, fileMenu(entry));
  });
  actions.append(more);

  li.append(main, size, modified, actions);
  return li;
}

function fileMenu(entry) {
  const items = [];
  if (entry.isDir) {
    items.push({ label: "Open", icon: "i-folder", action: () => loadFiles(entry.path, { push: true }) });
    items.push({ label: "Download as ZIP", icon: "i-download", action: () => triggerDownload(zipUrl(entry.path)) });
  } else {
    items.push({ label: "Preview", icon: "i-file", action: () => openPreview(entry) });
    items.push({ label: "Download", icon: "i-download", action: () => triggerDownload(fileUrl(entry.path)) });
  }
  items.push({ label: "Share link", icon: "i-share", action: () => openShare(entry) });
  items.push({ sep: true });
  items.push({ label: "Rename", icon: "i-text", action: () => renameEntry(entry) });
  items.push({ label: "Move", icon: "i-folder", action: () => openMove(entry) });
  items.push({ label: "Move to Trash", icon: "i-trash", danger: true, action: () => deleteEntry(entry) });
  return items;
}

function trashRow(item) {
  const kind = item.isDir ? "folder" : kindFor(item.name);
  const li = document.createElement("li");
  li.className = "file-row";

  const main = document.createElement("div");
  main.className = "row-main";
  main.append(iconBox(kind));
  const text = document.createElement("div");
  text.className = "row-text";
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = item.name;
  const sub = document.createElement("span");
  sub.className = "row-sub";
  const reason = item.reason === "retention" ? "Expired · " : "";
  sub.textContent = `${reason}from /${item.originalPath}`;
  text.append(name, sub);
  main.append(text);

  const size = document.createElement("span");
  size.className = "size";
  size.textContent = formatBytes(item.size);
  const modified = document.createElement("span");
  modified.className = "modified";
  modified.textContent = formatDate(item.deletedAt);

  const actions = document.createElement("div");
  actions.className = "row-actions";
  const more = iconButton("i-more", "Trash actions");
  more.addEventListener("click", (event) => {
    event.stopPropagation();
    openMenu(more, [
      { label: "Restore", icon: "i-restore", action: () => restoreTrash(item) },
      { label: "Delete forever", icon: "i-trash", danger: true, action: () => purgeTrash(item) },
    ]);
  });
  actions.append(more);
  li.append(main, size, modified, actions);
  return li;
}

function shareRow(item) {
  const kind = item.isDir ? "folder" : kindFor(basename(item.path));
  const li = document.createElement("li");
  li.className = "file-row";

  const main = document.createElement("div");
  main.className = "row-main";
  main.append(iconBox(kind));
  const text = document.createElement("div");
  text.className = "row-text";
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = basename(item.path);
  const sub = document.createElement("span");
  sub.className = "row-sub";
  sub.textContent = `/${item.path}`;
  text.append(name, sub);
  main.append(text);

  const size = document.createElement("span");
  size.className = "size";
  size.textContent = "Read-only";
  const modified = document.createElement("span");
  modified.className = "modified";
  modified.textContent = item.expiresAt ? `Expires ${formatDate(item.expiresAt)}` : "No expiry";

  const actions = document.createElement("div");
  actions.className = "row-actions";
  const more = iconButton("i-more", "Link actions");
  more.addEventListener("click", (event) => {
    event.stopPropagation();
    openMenu(more, [
      { label: "Copy link", icon: "i-copy", action: () => copyText(item.url) },
      { label: "Open link", icon: "i-external", action: () => window.open(item.url, "_blank", "noopener") },
      { label: "Revoke", icon: "i-trash", danger: true, action: () => revokeShare(item) },
    ]);
  });
  actions.append(more);
  li.append(main, size, modified, actions);
  return li;
}

function sortEntries(entries) {
  const { key, dir } = state.sort;
  return entries.slice().sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    let cmp = 0;
    if (key === "size") cmp = a.size - b.size;
    else if (key === "mod") cmp = new Date(a.modTime).getTime() - new Date(b.modTime).getTime();
    else cmp = a.name.localeCompare(b.name, undefined, { sensitivity: "base", numeric: true });
    return cmp * dir;
  });
}

function updateSortIndicators() {
  $$(".sort-btn").forEach((btn) => {
    btn.classList.toggle("active", btn.dataset.sort === state.sort.key);
    const base = btn.dataset.sort === "name" ? "Name" : btn.dataset.sort === "size" ? "Size" : "Modified";
    btn.textContent = btn.dataset.sort === state.sort.key ? `${base} ${state.sort.dir > 0 ? "↑" : "↓"}` : base;
  });
}

async function onSearchInput() {
  if (state.view !== "files") return;
  const query = els.search.value.trim();
  if (!query) {
    state.searchResults = null;
    state.searchQuery = "";
    renderFiles();
    return;
  }
  const requested = query;
  const path = state.path;
  await new Promise((resolve) => setTimeout(resolve, 250));
  if (els.search.value.trim() !== requested || state.path !== path) return;
  try {
    const data = await api(`/api/search?${new URLSearchParams({ path, q: requested })}`);
    if (els.search.value.trim() !== requested || state.path !== path) return;
    state.searchResults = data.entries || [];
    state.searchQuery = requested;
    renderFiles();
  } catch (err) {
    toast(err.message, "error");
  }
}

function promptNewFolder() {
  openInput({
    title: "New folder",
    description: "Create a folder in the current location.",
    label: "Folder name",
    value: "",
    submitLabel: "Create",
    onSubmit: async (name) => {
      await api("/api/mkdir", { method: "POST", body: { path: state.path, name } });
      toast("Folder created", "success");
      await loadFiles(state.path, { push: false });
    },
  });
}

function renameEntry(entry) {
  openInput({
    title: `Rename “${entry.name}”`,
    description: "Enter a new name for this item.",
    label: "Name",
    value: entry.name,
    submitLabel: "Rename",
    onSubmit: async (name) => {
      await api("/api/rename", { method: "POST", body: { path: entry.path, name } });
      toast("Renamed", "success");
      await loadFiles(state.path, { push: false });
    },
  });
}

async function deleteEntry(entry) {
  const ok = await confirmAction({
    title: "Move to Trash?",
    message: `“${entry.name}” will be moved to Trash. You can restore it later.`,
    okLabel: "Move to Trash",
  });
  if (!ok) return;
  try {
    await api("/api/delete", { method: "POST", body: { paths: [entry.path] } });
    toast("Moved to Trash", "success");
    await refreshSession();
    await loadFiles(state.path, { push: false });
  } catch (err) {
    toast(err.message, "error");
  }
}

function openInput({ title, description, label, value, submitLabel, onSubmit }) {
  showDialog(els.dialogInput);
  els.inputTitle.textContent = title;
  els.inputDesc.textContent = description || "";
  els.inputLabel.textContent = label;
  els.inputValue.value = value || "";
  els.inputSubmit.textContent = submitLabel || "Save";
  els.inputForm.onsubmit = async (event) => {
    event.preventDefault();
    const name = els.inputValue.value.trim();
    if (!name) {
      els.inputValue.focus();
      return;
    }
    els.inputSubmit.disabled = true;
    try {
      await onSubmit(name);
      els.dialogInput.close();
    } catch (err) {
      toast(err.message, "error");
    } finally {
      els.inputSubmit.disabled = false;
    }
  };
  setTimeout(() => {
    els.inputValue.focus();
    const dot = els.inputValue.value.lastIndexOf(".");
    if (dot > 0) els.inputValue.setSelectionRange(0, dot);
    else els.inputValue.select();
  }, 60);
}

function confirmAction({ title, message, okLabel = "Delete" }) {
  return new Promise((resolve) => {
    showDialog(els.dialogConfirm);
    els.confirmTitle.textContent = title;
    els.confirmMessage.textContent = message;
    els.confirmOk.textContent = okLabel;
    let settled = false;
    const onClose = () => {
      els.dialogConfirm.removeEventListener("close", onClose);
      if (!settled) resolve(false);
    };
    els.dialogConfirm.addEventListener("close", onClose);
    els.confirmOk.onclick = () => {
      settled = true;
      els.dialogConfirm.close();
      resolve(true);
    };
  });
}

async function openMove(entry) {
  state.move = { source: entry, path: "", busy: false };
  els.moveTitle.textContent = `Move “${entry.name}”`;
  showDialog(els.dialogMove);
  els.moveOk.disabled = false;
  await loadMoveDir("");
}

async function loadMoveDir(path) {
  state.move.path = path;
  try {
    const data = await api(`/api/list?${new URLSearchParams({ path })}`);
    renderMoveCrumbs(path);
    renderMoveList((data.entries || []).filter((e) => e.isDir));
  } catch (err) {
    toast(err.message, "error");
  }
}

function renderMoveCrumbs(path) {
  els.moveCrumbs.replaceChildren();
  const root = document.createElement("button");
  root.type = "button";
  root.textContent = "Files";
  if (!path) root.classList.add("current");
  else root.addEventListener("click", () => loadMoveDir(""));
  els.moveCrumbs.append(root);
  if (!path) return;
  let prefix = "";
  path.split("/").filter(Boolean).forEach((part, index, parts) => {
    const sep = document.createElement("span");
    sep.className = "sep";
    sep.textContent = "/";
    els.moveCrumbs.append(sep);
    prefix = prefix ? `${prefix}/${part}` : part;
    if (index === parts.length - 1) {
      const current = document.createElement("span");
      current.className = "current";
      current.textContent = part;
      els.moveCrumbs.append(current);
    } else {
      const target = prefix;
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = part;
      button.addEventListener("click", () => loadMoveDir(target));
      els.moveCrumbs.append(button);
    }
  });
}

function renderMoveList(folders) {
  els.moveList.replaceChildren();
  const source = state.move.source;
  if (state.move.path) {
    const parent = state.move.path.split("/").slice(0, -1).join("/");
    const up = document.createElement("li");
    up.append(icon("i-chevron", "icon small"));
    const label = document.createElement("span");
    label.textContent = "..";
    up.append(label);
    up.addEventListener("click", () => loadMoveDir(parent));
    els.moveList.append(up);
  }
  for (const folder of folders) {
    const li = document.createElement("li");
    li.append(icon("i-folder", "icon small"));
    const label = document.createElement("span");
    label.textContent = folder.name;
    li.append(label);
    const blocked = source && source.isDir && (folder.path === source.path || folder.path.startsWith(`${source.path}/`));
    if (blocked) {
      li.classList.add("disabled");
    } else {
      li.addEventListener("click", () => loadMoveDir(folder.path));
    }
    els.moveList.append(li);
  }
  if (!folders.length && !state.move.path) {
    const empty = document.createElement("li");
    empty.className = "move-empty";
    empty.textContent = "No folders";
    els.moveList.append(empty);
  }
}

async function moveHere() {
  const source = state.move.source;
  if (!source) return;
  if (source.isDir && (state.move.path === source.path || state.move.path.startsWith(`${source.path}/`))) {
    toast("Cannot move a folder into itself.", "error");
    return;
  }
  els.moveOk.disabled = true;
  try {
    await api("/api/move", { method: "POST", body: { path: source.path, dest: state.move.path } });
    els.dialogMove.close();
    toast("Moved", "success");
    await loadFiles(state.path, { push: false });
  } catch (err) {
    toast(err.message, "error");
  } finally {
    els.moveOk.disabled = false;
  }
}

function openShare(entry) {
  state.share = { source: entry, url: "" };
  els.shareTitle.textContent = `Share “${entry.name}”`;
  els.shareResult.hidden = true;
  els.shareExpiryField.hidden = false;
  els.shareCreate.hidden = false;
  els.shareCreate.disabled = false;
  els.shareUrl.value = "";
  els.shareOpen.removeAttribute("href");
  showDialog(els.dialogShare);
}

async function createShare() {
  if (!state.share.source) return;
  els.shareCreate.disabled = true;
  try {
    const hours = Number(els.shareExpiry.value);
    const data = await api("/api/shares", {
      method: "POST",
      body: { path: state.share.source.path, expiresInHours: hours },
    });
    state.share.url = data.url;
    els.shareUrl.value = data.url;
    els.shareOpen.href = data.url;
    els.shareResult.hidden = false;
    els.shareExpiryField.hidden = true;
    els.shareCreate.hidden = true;
    toast("Share link created", "success");
  } catch (err) {
    toast(err.message, "error");
  } finally {
    els.shareCreate.disabled = false;
  }
}

async function restoreTrash(item) {
  try {
    await api("/api/trash/restore", { method: "POST", body: { id: item.id } });
    toast("Restored", "success");
    await refreshSession();
    await loadTrash(false);
  } catch (err) {
    toast(err.message, "error");
  }
}

async function purgeTrash(item) {
  const ok = await confirmAction({
    title: "Delete forever?",
    message: `“${item.name}” will be permanently deleted. This cannot be undone.`,
    okLabel: "Delete forever",
  });
  if (!ok) return;
  try {
    await api("/api/trash/purge", { method: "POST", body: { id: item.id } });
    toast("Deleted permanently", "success");
    await refreshSession();
    await loadTrash(false);
  } catch (err) {
    toast(err.message, "error");
  }
}

async function emptyTrash() {
  const ok = await confirmAction({
    title: "Empty Trash?",
    message: "All items in Trash will be permanently deleted. This cannot be undone.",
    okLabel: "Empty Trash",
  });
  if (!ok) return;
  try {
    await api("/api/trash/empty", { method: "POST", body: {} });
    toast("Trash emptied", "success");
    await refreshSession();
    await loadTrash(false);
  } catch (err) {
    toast(err.message, "error");
  }
}

async function revokeShare(item) {
  const ok = await confirmAction({
    title: "Revoke link?",
    message: "The link will stop working immediately.",
    okLabel: "Revoke",
  });
  if (!ok) return;
  try {
    await api(`/api/shares/${encodeURIComponent(item.token)}`, { method: "DELETE" });
    toast("Link revoked", "success");
    await loadShares(false);
  } catch (err) {
    toast(err.message, "error");
  }
}

function openPreview(entry) {
  els.previewTitle.textContent = entry.name;
  els.previewMeta.textContent = `${formatBytes(entry.size)} · ${formatDate(entry.modTime)}`;
  els.previewDownload.href = fileUrl(entry.path);
  els.previewBody.replaceChildren();
  const src = fileUrl(entry.path, true);

  if (entry.kind === "image") {
    const img = document.createElement("img");
    img.alt = entry.name;
    img.src = src;
    els.previewBody.append(img);
  } else if (entry.kind === "video") {
    const video = document.createElement("video");
    video.controls = true;
    video.autoplay = true;
    video.src = src;
    els.previewBody.append(video);
  } else if (entry.kind === "audio") {
    const audio = document.createElement("audio");
    audio.controls = true;
    audio.autoplay = true;
    audio.src = src;
    els.previewBody.append(audio);
  } else if (entry.kind === "pdf") {
    const frame = document.createElement("iframe");
    frame.title = entry.name;
    frame.src = src;
    els.previewBody.append(frame);
  } else if (entry.kind === "text") {
    if (entry.size > 1024 * 1024) {
      els.previewBody.append(note("This file is too large to preview. Download it to view the contents."));
    } else {
      const pre = document.createElement("pre");
      pre.textContent = "Loading…";
      els.previewBody.append(pre);
      fetch(src, { credentials: "same-origin" })
        .then((res) => {
          if (!res.ok) throw new Error("Could not load preview.");
          return res.text();
        })
        .then((text) => {
          pre.textContent = text;
        })
        .catch(() => {
          pre.textContent = "Could not load preview.";
        });
    }
  } else {
    els.previewBody.append(note("No preview available for this file type. Download it to open it."));
  }
  showDialog(els.dialogPreview);
}

function note(text) {
  const div = document.createElement("div");
  div.className = "preview-note";
  div.textContent = text;
  return div;
}

function enqueueFiles(fileList) {
  if (state.view !== "files") {
    toast("Switch back to Files before uploading.", "error");
    return;
  }
  const max = Number(state.session?.maxUploadBytes || 5 * 1024 * 1024 * 1024);
  for (const file of Array.from(fileList || [])) {
    if (file.size > max) {
      toast(`${file.name} is larger than the ${formatBytes(max)} upload limit.`, "error");
      continue;
    }
    const id = (crypto.randomUUID && crypto.randomUUID()) || `${Date.now()}-${Math.random()}`;
    state.uploads.set(id, {
      id,
      file,
      name: file.name,
      size: file.size,
      dir: state.path,
      progress: 0,
      status: "queued",
      error: "",
    });
    state.uploadQueue.push(id);
  }
  renderUploads();
  pumpUploads();
}

function pumpUploads() {
  while (state.activeUploads < 2) {
    const id = state.uploadQueue.shift();
    if (!id) break;
    const item = state.uploads.get(id);
    if (!item) continue;
    startUpload(item);
  }
}

function startUpload(item) {
  state.activeUploads++;
  item.status = "uploading";
  updateUploadRow(item);
  const xhr = new XMLHttpRequest();
  xhr.open("POST", `/api/upload?${new URLSearchParams({ path: item.dir })}`);
  xhr.setRequestHeader("X-Fileshare", "1");
  xhr.upload.onprogress = (event) => {
    if (!event.lengthComputable) return;
    item.progress = (event.loaded / event.total) * 100;
    updateUploadRow(item);
  };
  xhr.onload = () => {
    state.activeUploads = Math.max(0, state.activeUploads - 1);
    if (xhr.status >= 200 && xhr.status < 300) {
      item.status = "done";
      item.progress = 100;
      scheduleReload(item.dir);
    } else if (xhr.status === 401) {
      location.replace("/login");
      return;
    } else {
      item.status = "error";
      try {
        const data = JSON.parse(xhr.responseText);
        item.error = data.error || `Upload failed (${xhr.status})`;
      } catch {
        item.error = `Upload failed (${xhr.status})`;
      }
      toast(`${item.name}: ${item.error}`, "error");
    }
    updateUploadRow(item);
    pumpUploads();
    if (item.status === "done") refreshSession();
  };
  xhr.onerror = () => {
    state.activeUploads = Math.max(0, state.activeUploads - 1);
    item.status = "error";
    item.error = "Network error";
    toast(`${item.name}: network error`, "error");
    updateUploadRow(item);
    pumpUploads();
  };
  xhr.onabort = () => {
    state.activeUploads = Math.max(0, state.activeUploads - 1);
    item.status = "error";
    item.error = "Cancelled";
    updateUploadRow(item);
    pumpUploads();
  };
  item.xhr = xhr;
  const form = new FormData();
  form.append("file", item.file, item.name);
  xhr.send(form);
}

function scheduleReload(dir) {
  if (state.view !== "files" || state.path !== dir) return;
  clearTimeout(state.reloadTimer);
  state.reloadTimer = setTimeout(() => {
    if (state.view === "files" && state.path === dir) loadFiles(dir, { push: false });
  }, 350);
}

function renderUploads() {
  const items = Array.from(state.uploads.values());
  els.uploadTray.hidden = items.length === 0;
  els.uploadList.replaceChildren();
  state.uploadEls.clear();
  for (const item of items) els.uploadList.append(uploadRow(item));
}

function uploadRow(item) {
  const li = document.createElement("li");
  li.className = "upload-item";
  li.dataset.id = item.id;
  const line = document.createElement("div");
  line.className = "upload-line";
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = item.name;
  name.title = item.name;
  const status = document.createElement("span");
  status.className = "status";
  const progress = document.createElement("div");
  progress.className = "progress";
  const bar = document.createElement("span");
  progress.append(bar);
  line.append(name, status);
  li.append(line, progress);
  state.uploadEls.set(item.id, { li, status, bar });
  updateUploadRow(item);
  return li;
}

function updateUploadRow(item) {
  let refs = state.uploadEls.get(item.id);
  if (!refs) {
    if (state.uploads.has(item.id)) {
      renderUploads();
      refs = state.uploadEls.get(item.id);
    }
    if (!refs) return;
  }
  refs.li.classList.toggle("done", item.status === "done");
  refs.li.classList.toggle("error", item.status === "error");
  if (item.status === "queued") refs.status.textContent = "Queued";
  else if (item.status === "uploading") refs.status.textContent = `${item.progress.toFixed(0)}%`;
  else if (item.status === "done") refs.status.textContent = "Done";
  else refs.status.textContent = item.error || "Failed";
  refs.bar.style.width = `${Math.max(0, Math.min(100, item.progress))}%`;
}

function clearFinishedUploads() {
  for (const [id, item] of state.uploads) {
    if (item.status === "done" || item.status === "error") state.uploads.delete(id);
  }
  renderUploads();
}

function openMenu(anchor, items) {
  closeMenu();
  const menu = document.createElement("div");
  menu.className = "row-menu";
  for (const item of items) {
    if (item.sep) {
      const sep = document.createElement("div");
      sep.className = "menu-sep";
      menu.append(sep);
      continue;
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = `menu-item${item.danger ? " danger" : ""}`;
    button.append(icon(item.icon, "icon"));
    const label = document.createElement("span");
    label.textContent = item.label;
    button.append(label);
    button.addEventListener("click", (event) => {
      event.stopPropagation();
      closeMenu();
      Promise.resolve(item.action()).catch((err) => toast(err.message, "error"));
    });
    menu.append(button);
  }
  els.menuRoot.append(menu);
  const rect = anchor.getBoundingClientRect();
  const menuRect = menu.getBoundingClientRect();
  const left = Math.max(10, Math.min(rect.right - menuRect.width, window.innerWidth - menuRect.width - 10));
  let top = rect.bottom + 6;
  if (top + menuRect.height > window.innerHeight - 10) {
    top = Math.max(10, rect.top - menuRect.height - 6);
  }
  menu.style.left = `${left}px`;
  menu.style.top = `${top}px`;

  const close = (event) => {
    if (event && menu.contains(event.target)) return;
    closeMenu();
  };
  document.addEventListener("click", close, true);
  window.addEventListener("resize", closeMenu, { once: true });
  state.menuCleanup = () => {
    document.removeEventListener("click", close, true);
    menu.remove();
  };
}

function closeMenu() {
  if (state.menuCleanup) {
    state.menuCleanup();
    state.menuCleanup = null;
  }
}

function showDialog(dialog) {
  for (const other of $$("dialog[open]")) {
    if (other !== dialog) other.close();
  }
  if (!dialog.open) dialog.showModal();
}

function triggerDownload(url) {
  const a = document.createElement("a");
  a.href = url;
  a.download = "";
  document.body.append(a);
  a.click();
  a.remove();
}

function fileUrl(path, inline = false) {
  const params = new URLSearchParams({ path });
  if (inline) params.set("inline", "1");
  return `/api/download?${params}`;
}

function zipUrl(path) {
  return `/api/zip?${new URLSearchParams({ path })}`;
}

function icon(id, className = "icon") {
  const svg = document.createElementNS(svgNS, "svg");
  svg.setAttribute("class", className);
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS(svgNS, "use");
  use.setAttribute("href", `#${id}`);
  svg.append(use);
  return svg;
}

function iconButton(iconID, label) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "icon-btn";
  button.setAttribute("aria-label", label);
  button.title = label;
  button.append(icon(iconID));
  return button;
}

function iconBox(kind) {
  const span = document.createElement("span");
  span.className = `ico ${kind}`;
  span.append(icon(iconFor(kind)));
  return span;
}

function iconFor(kind) {
  switch (kind) {
    case "folder": return "i-folder";
    case "image": return "i-image";
    case "video": return "i-video";
    case "audio": return "i-audio";
    case "pdf": return "i-pdf";
    case "text": return "i-text";
    case "archive": return "i-archive";
    default: return "i-file";
  }
}

function kindFor(name) {
  const ext = (name.split(".").pop() || "").toLowerCase();
  if (["jpg", "jpeg", "png", "gif", "webp", "avif", "bmp", "heic"].includes(ext)) return "image";
  if (["mp4", "webm", "mov", "mkv", "m4v"].includes(ext)) return "video";
  if (["mp3", "wav", "ogg", "flac", "m4a", "aac"].includes(ext)) return "audio";
  if (ext === "pdf") return "pdf";
  if (["zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "rar"].includes(ext)) return "archive";
  if (["txt", "md", "markdown", "json", "yaml", "yml", "toml", "ini", "conf", "csv", "log",
    "go", "js", "ts", "tsx", "jsx", "css", "html", "htm", "xml", "sh", "rs", "c", "h",
    "cpp", "hpp", "py", "java", "scala", "nix"].includes(ext)) return "text";
  return "file";
}

function basename(path) {
  const parts = path.split("/").filter(Boolean);
  return parts.length ? parts[parts.length - 1] : "Files";
}

function formatBytes(n) {
  n = Number(n);
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let value = n / 1024;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index++;
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[index]}`;
}

function formatDate(value) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return dateFmt.format(date);
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast("Copied to clipboard", "success");
  } catch {
    const area = document.createElement("textarea");
    area.value = text;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.append(area);
    area.select();
    try {
      document.execCommand("copy");
      toast("Copied to clipboard", "success");
    } catch {
      toast("Could not copy the link.", "error");
    }
    area.remove();
  }
}

function toast(message, type = "") {
  const div = document.createElement("div");
  div.className = `toast${type ? ` ${type}` : ""}`;
  div.textContent = message;
  els.toasts.append(div);
  setTimeout(() => {
    div.style.opacity = "0";
    div.style.transform = "translateY(4px)";
    div.style.transition = "opacity .2s ease, transform .2s ease";
    setTimeout(() => div.remove(), 220);
  }, 3800);
}

async function logout() {
  try {
    await api("/api/logout", { method: "POST", body: {} });
  } catch {
    /* ignore */
  }
  location.replace("/login");
}
