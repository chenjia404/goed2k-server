package ed2ksrv

import (
	"encoding/base64"
	"encoding/json"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

//go:embed templates/admin.html
var adminHTMLTemplateFS embed.FS

var adminPageTemplate *template.Template

func init() {
	b, err := adminHTMLTemplateFS.ReadFile("templates/admin.html")
	if err != nil {
		panic(err)
	}
	adminPageTemplate = template.Must(template.New("admin-ui").Parse(string(b)))
}

type adminUIData struct {
	ServerName   string
	Description  string
	TokenEnabled bool
	Lang         string
	IsEN         bool
	IsZH         bool
	T            adminHTMLStrings
	I18NBase64   string
}

func (s *Server) handleAdminUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/clients/") && !strings.HasPrefix(r.URL.Path, "/files/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	lang := resolveAdminLocale(r)
	htmlStr, jsStr := getAdminLocalePack(lang)
	raw, err := json.Marshal(jsStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshal admin i18n: %v", err), http.StatusInternalServerError)
		return
	}
	data := adminUIData{
		ServerName:   s.cfg.ServerName,
		Description:  s.cfg.ServerDescription,
		TokenEnabled: strings.TrimSpace(s.cfg.AdminToken) != "",
		Lang:         lang,
		IsEN:         lang == "en",
		IsZH:         lang == "zh",
		T:            htmlStr,
		I18NBase64:   base64.StdEncoding.EncodeToString(raw),
	}
	if err := adminPageTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("render admin ui: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handleAdminJS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.js" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write([]byte(adminUIScript))
}

func (s *Server) handleAdminCSS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.css" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(adminUIStyles))
}

const adminUIScript = `(() => {
  const i18n = JSON.parse(atob(document.getElementById('admin-i18n').dataset.b64));
  function i18nFmt(tpl, vars) {
    var s = tpl;
    if (!vars) {
      return s;
    }
    Object.keys(vars).forEach(function (k) {
      s = s.split('{' + k + '}').join(String(vars[k]));
    });
    return s;
  }
  const body = document.body;
  const tokenEnabled = body.dataset.tokenEnabled === 'true';
  const loginPanel = document.getElementById('loginPanel');
  const dashboard = document.getElementById('dashboard');
  const toast = document.getElementById('toast');
  const refreshButton = document.getElementById('refreshButton');
  const logoutButton = document.getElementById('logoutButton');
  const loginForm = document.getElementById('loginForm');
  const fileForm = document.getElementById('fileForm');
  const clientSearch = document.getElementById('clientSearch');
  const fileSearch = document.getElementById('fileSearch');
  const fileTypeFilter = document.getElementById('fileTypeFilter');
  const detailView = document.getElementById('detailView');
  const clientsBody = document.getElementById('clientsBody');
  const filesBody = document.getElementById('filesBody');
  const auditBody = document.getElementById('auditBody');
  const tokenInput = document.getElementById('adminToken');
  const deleteSelectedButton = document.getElementById('deleteSelectedButton');

  const state = {
    clientsPage: 1,
    filesPage: 1,
    auditPage: 1,
    clientsPerPage: 10,
    filesPerPage: 10,
    auditPerPage: 10,
    selectedFiles: new Set()
  };

  let adminToken = tokenEnabled ? (window.localStorage.getItem('goed2k_admin_token') || '') : '';

  function setToast(message, isError) {
    toast.textContent = message;
    toast.classList.remove('hidden', 'error');
    if (isError) {
      toast.classList.add('error');
    }
    window.clearTimeout(setToast.timer);
    setToast.timer = window.setTimeout(() => toast.classList.add('hidden'), 2600);
  }

  function setAuthState(authenticated) {
    loginPanel.classList.toggle('hidden', authenticated);
    dashboard.classList.toggle('hidden', !authenticated);
    logoutButton.classList.toggle('hidden', !tokenEnabled || !authenticated);
    refreshButton.disabled = !authenticated;
  }

  async function apiFetch(path, options) {
    const requestOptions = options || {};
    const headers = new Headers(requestOptions.headers || {});
    if (adminToken) {
      headers.set('X-Admin-Token', adminToken);
    }
    if (requestOptions.body && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json');
    }
    const response = await fetch(path, { ...requestOptions, headers });
    const payload = await response.json().catch(() => ({ ok: false, error: i18n.invalidResponse }));
    if (!response.ok || !payload.ok) {
      throw new Error(payload.error || ('request failed: ' + response.status));
    }
    return payload;
  }

  function formatBytes(value) {
    const size = Number(value || 0);
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let index = 0;
    let current = size;
    while (current >= 1024 && index < units.length - 1) {
      current /= 1024;
      index += 1;
    }
    return current.toFixed(index === 0 ? 0 : 1) + ' ' + units[index];
  }

  function formatTime(value) {
    if (!value) {
      return '-';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return value;
    }
    return date.toLocaleString(i18n.dateLocale, { hour12: false });
  }

  function updatePager(idPrefix, meta) {
    const currentPage = Number(meta.page || 1);
    const perPage = Number(meta.per_page || 10);
    const total = Number(meta.total || 0);
    const totalPages = Math.max(1, Math.ceil(total / perPage));
    document.getElementById(idPrefix + 'PageInfo').textContent = i18nFmt(i18n.pagerFmt, { cur: currentPage, total: totalPages });
    document.getElementById(idPrefix + 'Prev').disabled = currentPage <= 1;
    document.getElementById(idPrefix + 'Next').disabled = currentPage >= totalPages;
  }

  function showDetail(data) {
    detailView.textContent = JSON.stringify(data, null, 2);
  }

  function pushRoute(path) {
    if (window.location.pathname !== path) {
      window.history.pushState({}, '', path);
    }
  }

  async function loadStats() {
    const payload = await apiFetch('/api/stats');
    const data = payload.data;
    document.getElementById('statClients').textContent = data.current_clients;
    document.getElementById('statConnections').textContent = i18nFmt(i18n.statConnectionsFmt, { n: data.total_connections });
    document.getElementById('statFiles').textContent = data.current_files;
    document.getElementById('statRegistered').textContent = i18nFmt(i18n.statRegisteredFmt, { reg: data.files_registered, rem: data.files_removed });
    document.getElementById('statSearches').textContent = data.search_requests;
    document.getElementById('statSearchEntries').textContent = i18nFmt(i18n.statSearchEntriesFmt, { n: data.search_result_entries });
    document.getElementById('statTraffic').textContent = formatBytes((data.inbound_bytes || 0) + (data.outbound_bytes || 0));
    document.getElementById('statPackets').textContent = i18nFmt(i18n.statPacketsFmt, { inPkts: data.inbound_packets, outPkts: data.outbound_packets });
  }

  async function loadClients() {
    const query = new URLSearchParams({
      page: String(state.clientsPage),
      per_page: String(state.clientsPerPage),
      sort: 'last_seen_at'
    });
    if (clientSearch.value.trim()) {
      query.set('search', clientSearch.value.trim());
    }
    const payload = await apiFetch('/api/clients?' + query.toString());
    const data = payload.data;
    clientsBody.innerHTML = '';
    data.forEach((client) => {
      const row = document.createElement('tr');
      row.innerHTML =
        '<td>' + client.client_id + '</td>' +
        '<td><a class="row-link" href="/clients/' + client.client_id + '">' + (client.client_name || '-') + '</a></td>' +
        '<td>' + (client.remote_address || '-') + '</td>' +
        '<td>' + (client.listen_endpoint || '-') + '</td>' +
        '<td>' + formatTime(client.last_seen_at) + '</td>';
      row.querySelector('a').addEventListener('click', function (event) {
        event.preventDefault();
        pushRoute('/clients/' + client.client_id);
        loadClientDetail(client.client_id).catch((error) => setToast(error.message, true));
      });
      row.addEventListener('click', function () { showDetail(client); });
      clientsBody.appendChild(row);
    });
    if (!data.length) {
      clientsBody.innerHTML = '<tr><td colspan="5" class="empty">' + i18n.emptyClients + '</td></tr>';
    }
    updatePager('clients', payload.meta || {});
  }

  async function loadClientDetail(clientID) {
    const payload = await apiFetch('/api/clients/' + encodeURIComponent(String(clientID)));
    showDetail(payload.data);
  }

  async function deleteFile(hash) {
    await apiFetch('/api/files/' + encodeURIComponent(hash), { method: 'DELETE' });
    state.selectedFiles.delete(hash);
    setToast('共享文件已删除');
    await refreshAll();
  }

  async function batchDeleteSelected() {
    const hashes = Array.from(state.selectedFiles);
    if (!hashes.length) {
      setToast(i18n.toastNoSelection, true);
      return;
    }
    await apiFetch('/api/files/batch-delete', {
      method: 'POST',
      body: JSON.stringify({ hashes: hashes })
    });
    state.selectedFiles.clear();
    setToast(i18n.toastBatchDone);
    await refreshAll();
  }

  async function loadFiles() {
    const query = new URLSearchParams({
      page: String(state.filesPage),
      per_page: String(state.filesPerPage),
      sort: 'sources'
    });
    if (fileSearch.value.trim()) {
      query.set('search', fileSearch.value.trim());
    }
    if (fileTypeFilter.value.trim()) {
      query.set('file_type', fileTypeFilter.value.trim());
    }
    const payload = await apiFetch('/api/files?' + query.toString());
    const data = payload.data;
    filesBody.innerHTML = '';
    data.forEach((file) => {
      const checked = state.selectedFiles.has(file.hash) ? ' checked' : '';
      const row = document.createElement('tr');
      row.innerHTML =
        '<td><input class="file-selector" data-hash="' + file.hash + '" type="checkbox"' + checked + '></td>' +
        '<td><a class="row-link" href="/files/' + file.hash + '">' + (file.name || '-') + '</a></td>' +
        '<td>' + (file.file_type || '-') + '</td>' +
        '<td>' + formatBytes(file.size) + '</td>' +
        '<td>' + (file.sources || 0) + '</td>' +
        '<td><button type="button" class="ghost danger">' + i18n.btnDelete + '</button></td>';
      row.querySelector('a').addEventListener('click', function (event) {
        event.preventDefault();
        pushRoute('/files/' + file.hash);
        loadFileDetail(file.hash).catch((error) => setToast(error.message, true));
      });
      row.querySelector('.file-selector').addEventListener('change', function (event) {
        if (event.target.checked) {
          state.selectedFiles.add(file.hash);
        } else {
          state.selectedFiles.delete(file.hash);
        }
      });
      row.querySelector('button').addEventListener('click', function (event) {
        event.stopPropagation();
        deleteFile(file.hash).catch((error) => setToast(error.message, true));
      });
      row.addEventListener('click', function (event) {
        if (event.target.tagName !== 'BUTTON' && event.target.tagName !== 'INPUT' && event.target.tagName !== 'A') {
          showDetail(file);
        }
      });
      filesBody.appendChild(row);
    });
    if (!data.length) {
      filesBody.innerHTML = '<tr><td colspan="6" class="empty">' + i18n.emptyFiles + '</td></tr>';
    }
    updatePager('files', payload.meta || {});
  }

  async function loadFileDetail(hash) {
    const payload = await apiFetch('/api/files/' + encodeURIComponent(hash));
    showDetail(payload.data);
  }

  async function loadAudit() {
    const query = new URLSearchParams({
      page: String(state.auditPage),
      per_page: String(state.auditPerPage)
    });
    const payload = await apiFetch('/api/audit?' + query.toString());
    const data = payload.data;
    auditBody.innerHTML = '';
    data.forEach((entry) => {
      const row = document.createElement('tr');
      row.innerHTML =
        '<td>' + formatTime(entry.time) + '</td>' +
        '<td>' + entry.action + '</td>' +
        '<td>' + entry.resource + '</td>' +
        '<td>' + (entry.resource_id || '-') + '</td>' +
        '<td>' + (entry.remote_addr || '-') + '</td>' +
        '<td>' + entry.status + '</td>';
      row.addEventListener('click', function () { showDetail(entry); });
      auditBody.appendChild(row);
    });
    if (!data.length) {
      auditBody.innerHTML = '<tr><td colspan="6" class="empty">' + i18n.emptyAudit + '</td></tr>';
    }
    updatePager('audit', payload.meta || {});
  }

  async function loadRouteDetail() {
    const path = window.location.pathname;
    if (path.indexOf('/clients/') === 0) {
      await loadClientDetail(path.slice('/clients/'.length));
      return;
    }
    if (path.indexOf('/files/') === 0) {
      await loadFileDetail(path.slice('/files/'.length));
    }
  }

  async function refreshAll() {
    try {
      await Promise.all([loadStats(), loadClients(), loadFiles(), loadAudit()]);
      await loadRouteDetail();
      setAuthState(true);
    } catch (error) {
      if (tokenEnabled && /unauthorized/i.test(error.message)) {
        setAuthState(false);
        setToast(i18n.toastInvalidToken, true);
        return;
      }
      setToast(error.message, true);
    }
  }

  loginForm.addEventListener('submit', async function (event) {
    event.preventDefault();
    adminToken = tokenInput.value.trim();
    window.localStorage.setItem('goed2k_admin_token', adminToken);
    await refreshAll();
  });

  fileForm.addEventListener('submit', async function (event) {
    event.preventDefault();
    const form = new FormData(fileForm);
    const port = Number(form.get('port') || 0);
    const host = String(form.get('host') || '').trim();
    const payload = {
      hash: String(form.get('hash') || '').trim().toUpperCase(),
      name: String(form.get('name') || '').trim(),
      size: Number(form.get('size') || 0),
      file_type: String(form.get('file_type') || '').trim(),
      extension: String(form.get('extension') || '').trim(),
      endpoints: host && port ? [{ host: host, port: port }] : []
    };
    try {
      await apiFetch('/api/files', { method: 'POST', body: JSON.stringify(payload) });
      fileForm.reset();
      setToast(i18n.toastSaved);
      await refreshAll();
    } catch (error) {
      setToast(error.message, true);
    }
  });

  refreshButton.addEventListener('click', refreshAll);
  logoutButton.addEventListener('click', function () {
    adminToken = '';
    window.localStorage.removeItem('goed2k_admin_token');
    tokenInput.value = '';
    setAuthState(false);
    setToast(i18n.toastLoggedOut);
  });
  deleteSelectedButton.addEventListener('click', function () {
    batchDeleteSelected().catch((error) => setToast(error.message, true));
  });

  document.getElementById('clientsPrev').addEventListener('click', function () {
    if (state.clientsPage > 1) {
      state.clientsPage -= 1;
      loadClients().catch((error) => setToast(error.message, true));
    }
  });
  document.getElementById('clientsNext').addEventListener('click', function () {
    state.clientsPage += 1;
    loadClients().catch((error) => setToast(error.message, true));
  });
  document.getElementById('filesPrev').addEventListener('click', function () {
    if (state.filesPage > 1) {
      state.filesPage -= 1;
      loadFiles().catch((error) => setToast(error.message, true));
    }
  });
  document.getElementById('filesNext').addEventListener('click', function () {
    state.filesPage += 1;
    loadFiles().catch((error) => setToast(error.message, true));
  });
  document.getElementById('auditPrev').addEventListener('click', function () {
    if (state.auditPage > 1) {
      state.auditPage -= 1;
      loadAudit().catch((error) => setToast(error.message, true));
    }
  });
  document.getElementById('auditNext').addEventListener('click', function () {
    state.auditPage += 1;
    loadAudit().catch((error) => setToast(error.message, true));
  });

  let clientSearchTimer = 0;
  let fileSearchTimer = 0;
  clientSearch.addEventListener('input', function () {
    state.clientsPage = 1;
    window.clearTimeout(clientSearchTimer);
    clientSearchTimer = window.setTimeout(function () {
      loadClients().catch((error) => setToast(error.message, true));
    }, 180);
  });
  [fileSearch, fileTypeFilter].forEach(function (node) {
    node.addEventListener('input', function () {
      state.filesPage = 1;
      window.clearTimeout(fileSearchTimer);
      fileSearchTimer = window.setTimeout(function () {
        loadFiles().catch((error) => setToast(error.message, true));
      }, 180);
    });
  });
  window.addEventListener('popstate', function () {
    loadRouteDetail().catch((error) => setToast(error.message, true));
  });

  if (tokenEnabled) {
    tokenInput.value = adminToken;
    if (adminToken) {
      refreshAll();
    } else {
      setAuthState(false);
    }
  } else {
    setAuthState(true);
    refreshAll();
  }
})();`

const adminUIStyles = `:root {
  --bg: #f5efe4;
  --panel: rgba(255, 251, 245, 0.88);
  --panel-strong: rgba(255, 251, 245, 0.96);
  --ink: #182028;
  --muted: #5f6b72;
  --line: rgba(24, 32, 40, 0.12);
  --accent: #0e8f70;
  --accent-2: #d95d39;
  --accent-3: #2a5f9e;
  --shadow: 0 24px 60px rgba(42, 43, 56, 0.12);
  --radius: 24px;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  color: var(--ink);
  background:
    radial-gradient(circle at top left, rgba(14, 143, 112, 0.18), transparent 28%),
    radial-gradient(circle at 85% 10%, rgba(217, 93, 57, 0.16), transparent 24%),
    linear-gradient(180deg, #f7f0e7 0%, #ede2d5 100%);
  font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
}
button, input { font: inherit; }
.ambient { position: fixed; border-radius: 999px; filter: blur(20px); opacity: 0.38; pointer-events: none; }
.ambient-a { width: 320px; height: 320px; top: -80px; right: -40px; background: rgba(14, 143, 112, 0.16); }
.ambient-b { width: 280px; height: 280px; left: -60px; bottom: 8%; background: rgba(42, 95, 158, 0.14); }
.shell { position: relative; max-width: 1320px; margin: 0 auto; padding: 32px 20px 60px; }
.hero { display: flex; justify-content: space-between; gap: 24px; align-items: end; margin-bottom: 24px; }
.eyebrow, .panel-kicker { margin: 0 0 10px; letter-spacing: 0.12em; text-transform: uppercase; font-size: 12px; color: var(--accent-3); }
h1, h2 { margin: 0; font-family: "Space Grotesk", "IBM Plex Sans", sans-serif; }
h1 { font-size: clamp(2.4rem, 4vw, 4.6rem); line-height: 0.95; }
h2 { font-size: 1.4rem; }
.hero-copy, .muted { color: var(--muted); max-width: 60ch; }
.hero-actions, .head-actions { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.lang-switch { display: flex; gap: 8px; align-items: center; font-size: 13px; color: var(--muted); }
.lang-switch a { color: var(--muted); text-decoration: none; }
.lang-switch a:hover { color: var(--ink); }
.lang-switch a.active { color: var(--accent-3); font-weight: 600; }
.lang-sep { opacity: 0.45; user-select: none; }
button { border: 0; border-radius: 999px; padding: 12px 18px; background: var(--ink); color: #fff; cursor: pointer; transition: transform 140ms ease, opacity 140ms ease, background 140ms ease; }
button:hover { transform: translateY(-1px); }
button:disabled { opacity: 0.45; cursor: not-allowed; transform: none; }
button.ghost { background: rgba(24, 32, 40, 0.08); color: var(--ink); }
button.danger { background: rgba(217, 93, 57, 0.12); color: #7e2f19; }
.panel { background: var(--panel); backdrop-filter: blur(14px); border: 1px solid rgba(255, 255, 255, 0.4); border-radius: var(--radius); box-shadow: var(--shadow); padding: 22px; }
.login-panel { max-width: 720px; display: grid; grid-template-columns: 1.2fr 1fr; gap: 24px; align-items: center; }
.login-form, .file-form { display: grid; gap: 14px; }
label { display: grid; gap: 8px; color: var(--muted); }
input { width: 100%; border: 1px solid var(--line); background: var(--panel-strong); border-radius: 16px; padding: 12px 14px; color: var(--ink); }
.dashboard { display: grid; gap: 20px; }
.stats-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; }
.stat-card { position: relative; overflow: hidden; padding: 22px; border-radius: 22px; min-height: 150px; box-shadow: var(--shadow); color: #fff; }
.stat-card::after { content: ""; position: absolute; inset: auto -18% -40% auto; width: 180px; height: 180px; border-radius: 999px; background: rgba(255, 255, 255, 0.12); }
.stat-card span, .stat-card small, .stat-card strong { position: relative; z-index: 1; }
.stat-card strong { display: block; margin: 16px 0 12px; font-size: 2.6rem; font-family: "Space Grotesk", sans-serif; }
.accent-a { background: linear-gradient(135deg, #0e8f70, #126b6d); }
.accent-b { background: linear-gradient(135deg, #d95d39, #9d3d2a); }
.accent-c { background: linear-gradient(135deg, #2a5f9e, #1f4571); }
.accent-d { background: linear-gradient(135deg, #3c4454, #252b36); }
.content-grid { display: grid; grid-template-columns: 1.3fr 1.3fr 0.9fr; gap: 20px; }
.wide-panel { grid-column: span 2; }
.form-panel, .detail-panel { min-height: 420px; }
.audit-panel { grid-column: span 3; }
.panel-head { display: flex; justify-content: space-between; gap: 16px; margin-bottom: 18px; align-items: start; }
.compact-field { min-width: 180px; }
.compact-field span { font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; }
.table-wrap { overflow: auto; }
table { width: 100%; border-collapse: collapse; }
th, td { padding: 14px 12px; border-bottom: 1px solid var(--line); text-align: left; vertical-align: middle; }
tr { transition: background 140ms ease; }
tr:hover { background: rgba(14, 143, 112, 0.06); }
.empty { text-align: center; color: var(--muted); }
.row-link { color: var(--accent-3); text-decoration: none; }
.row-link:hover { text-decoration: underline; }
.pager { display: flex; gap: 12px; justify-content: flex-end; align-items: center; padding-top: 14px; }
.detail-view { margin: 0; height: calc(100% - 52px); min-height: 320px; padding: 16px; background: rgba(24, 32, 40, 0.92); color: #d2ffe6; border-radius: 18px; overflow: auto; font-family: "IBM Plex Mono", monospace; font-size: 13px; }
.toast { position: fixed; right: 20px; bottom: 20px; padding: 14px 18px; border-radius: 16px; background: rgba(24, 32, 40, 0.92); color: #fff; box-shadow: var(--shadow); }
.toast.error { background: rgba(126, 47, 25, 0.94); }
.hidden { display: none !important; }
@media (max-width: 1100px) {
  .stats-grid, .content-grid, .login-panel { grid-template-columns: 1fr 1fr; }
  .wide-panel, .audit-panel { grid-column: span 2; }
}
@media (max-width: 760px) {
  .shell { padding: 20px 14px 40px; }
  .hero, .panel-head, .hero-actions, .head-actions, .login-panel, .stats-grid, .content-grid { grid-template-columns: 1fr; display: grid; }
  .wide-panel, .audit-panel { grid-column: span 1; }
  .hero-actions, .head-actions, .pager { justify-content: stretch; }
  button { width: 100%; }
}`
