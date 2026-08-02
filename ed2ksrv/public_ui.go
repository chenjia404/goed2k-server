package ed2ksrv

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

//go:embed templates/public.html
var publicHTMLTemplateFS embed.FS

var publicPageTemplate *template.Template

func init() {
	b, err := publicHTMLTemplateFS.ReadFile("templates/public.html")
	if err != nil {
		panic(err)
	}
	publicPageTemplate = template.Must(template.New("public-ui").Parse(string(b)))
}

type publicUIData struct {
	ServerName string
	Lang       string
	IsEN       bool
	IsZH       bool
	T          publicHTMLStrings
	I18NBase64 string
}

func (s *Server) handlePublicUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	path := r.URL.Path
	if path != "/" && path != "/search" && !strings.HasPrefix(path, "/file/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	lang := resolvePublicLocale(r)
	htmlStr, jsStr := getPublicLocalePack(lang)
	raw, err := json.Marshal(jsStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshal public i18n: %v", err), http.StatusInternalServerError)
		return
	}
	data := publicUIData{
		ServerName: s.cfg.ServerName,
		Lang:       lang,
		IsEN:       lang == "en",
		IsZH:       lang == "zh",
		T:          htmlStr,
		I18NBase64: base64.StdEncoding.EncodeToString(raw),
	}
	if err := publicPageTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("render public ui: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handlePublicJS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.js" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write([]byte(publicUIScript))
}

func (s *Server) handlePublicCSS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/app.css" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(publicUIStyles))
}

type publicHTMLStrings struct {
	Title       string
	Subtitle    string
	SearchPH    string
	SearchBtn   string
	SortLabel   string
	SortName    string
	SortSize    string
	SortSources string
	ExtLabel    string
	TypeLabel   string
	Prev        string
	Next        string
	Back        string
	DetailTitle string
	HashLabel   string
	SizeLabel   string
	TypeField   string
	SourcesLbl  string
	CompleteLbl string
	PeersTitle  string
	Ed2kLink    string
	CopyBtn     string
	CopiedBtn   string
	NoResults   string
	Loading     string
	Error       string
}

type publicJSStrings struct {
	SearchPH    string
	SearchBtn   string
	SortName    string
	SortSize    string
	SortSources string
	Prev        string
	Next        string
	Back        string
	HashLabel   string
	SizeLabel   string
	TypeField   string
	SourcesLbl  string
	CompleteLbl string
	PeersTitle  string
	Ed2kLink    string
	CopyBtn     string
	CopiedBtn   string
	NoResults   string
	Loading     string
	Error       string
	PageInfo    string
}

func resolvePublicLocale(r *http.Request) string {
	if lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))); lang == "en" || lang == "zh" {
		return lang
	}
	accept := r.Header.Get("Accept-Language")
	if strings.Contains(strings.ToLower(accept), "zh") {
		return "zh"
	}
	return "zh"
}

func getPublicLocalePack(lang string) (publicHTMLStrings, publicJSStrings) {
	if lang == "en" {
		return publicHTMLStrings{
				Title: "ED2K Resource Search", Subtitle: "Search shared files on this server",
				SearchPH: "Enter keywords, e.g. ubuntu iso", SearchBtn: "Search",
				SortLabel: "Sort", SortName: "Name", SortSize: "Size", SortSources: "Sources",
				ExtLabel: "Extension", TypeLabel: "Type", Prev: "Previous", Next: "Next",
				Back: "Back to search", DetailTitle: "File Details",
				HashLabel: "Hash", SizeLabel: "Size", TypeField: "Type",
				SourcesLbl: "Sources", CompleteLbl: "Complete", PeersTitle: "Peer List",
				Ed2kLink: "ED2K Link", CopyBtn: "Copy", CopiedBtn: "Copied",
				NoResults: "No results found", Loading: "Loading...", Error: "Request failed",
			}, publicJSStrings{
				SearchPH: "Enter keywords, e.g. ubuntu iso", SearchBtn: "Search",
				SortName: "Name", SortSize: "Size", SortSources: "Sources",
				Prev: "Previous", Next: "Next", Back: "Back to search",
				HashLabel: "Hash", SizeLabel: "Size", TypeField: "Type",
				SourcesLbl: "Sources", CompleteLbl: "Complete", PeersTitle: "Peer List",
				Ed2kLink: "ED2K Link", CopyBtn: "Copy", CopiedBtn: "Copied",
				NoResults: "No results found", Loading: "Loading...", Error: "Request failed",
				PageInfo: "Page {page} / {pages} ({total} items)",
			}
	}
	return publicHTMLStrings{
			Title: "ED2K 资源搜索", Subtitle: "在本服务器上搜索共享文件",
			SearchPH: "输入关键词，例如 ubuntu iso", SearchBtn: "搜索",
			SortLabel: "排序", SortName: "名称", SortSize: "大小", SortSources: "来源数",
			ExtLabel: "扩展名", TypeLabel: "类型", Prev: "上一页", Next: "下一页",
			Back: "返回搜索", DetailTitle: "文件详情",
			HashLabel: "哈希", SizeLabel: "大小", TypeField: "类型",
			SourcesLbl: "来源数", CompleteLbl: "完整源", PeersTitle: "Peer 列表",
			Ed2kLink: "ED2K 链接", CopyBtn: "复制", CopiedBtn: "已复制",
			NoResults: "未找到结果", Loading: "加载中...", Error: "请求失败",
		}, publicJSStrings{
			SearchPH: "输入关键词，例如 ubuntu iso", SearchBtn: "搜索",
			SortName: "名称", SortSize: "大小", SortSources: "来源数",
			Prev: "上一页", Next: "下一页", Back: "返回搜索",
			HashLabel: "哈希", SizeLabel: "大小", TypeField: "类型",
			SourcesLbl: "来源数", CompleteLbl: "完整源", PeersTitle: "Peer 列表",
			Ed2kLink: "ED2K 链接", CopyBtn: "复制", CopiedBtn: "已复制",
			NoResults: "未找到结果", Loading: "加载中...", Error: "请求失败",
			PageInfo: "第 {page} / {pages} 页（共 {total} 条）",
		}
}

const publicUIStyles = `
:root {
  --bg: #0f1419;
  --surface: #1a2332;
  --border: #2d3a4d;
  --text: #e7ecf3;
  --muted: #8b9cb3;
  --accent: #3b82f6;
  --accent-hover: #2563eb;
  --success: #22c55e;
}
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  background: var(--bg);
  color: var(--text);
  min-height: 100vh;
  line-height: 1.5;
}
.container { max-width: 960px; margin: 0 auto; padding: 2rem 1rem; }
header { text-align: center; margin-bottom: 2rem; }
header h1 { font-size: 1.75rem; font-weight: 600; margin-bottom: 0.25rem; }
header p { color: var(--muted); font-size: 0.95rem; }
.search-box {
  display: flex; gap: 0.5rem; margin-bottom: 1.5rem;
}
.search-box input[type="text"] {
  flex: 1; padding: 0.65rem 1rem; border: 1px solid var(--border);
  border-radius: 8px; background: var(--surface); color: var(--text); font-size: 1rem;
}
.search-box input:focus { outline: none; border-color: var(--accent); }
.search-box button, .btn {
  padding: 0.65rem 1.25rem; border: none; border-radius: 8px;
  background: var(--accent); color: #fff; font-size: 0.95rem; cursor: pointer;
  transition: background 0.15s;
}
.search-box button:hover, .btn:hover { background: var(--accent-hover); }
.filters {
  display: flex; flex-wrap: wrap; gap: 0.75rem; margin-bottom: 1.5rem; align-items: center;
}
.filters label { color: var(--muted); font-size: 0.85rem; }
.filters select, .filters input {
  padding: 0.4rem 0.6rem; border: 1px solid var(--border); border-radius: 6px;
  background: var(--surface); color: var(--text); font-size: 0.85rem;
}
.results { list-style: none; }
.result-item {
  padding: 1rem; border: 1px solid var(--border); border-radius: 8px;
  margin-bottom: 0.5rem; background: var(--surface); cursor: pointer;
  transition: border-color 0.15s;
}
.result-item:hover { border-color: var(--accent); }
.result-item .name { font-weight: 500; margin-bottom: 0.25rem; word-break: break-all; }
.result-item .meta { color: var(--muted); font-size: 0.85rem; }
.result-item .meta span { margin-right: 1rem; }
.pagination {
  display: flex; justify-content: center; align-items: center; gap: 1rem; margin-top: 1.5rem;
}
.pagination button:disabled { opacity: 0.4; cursor: not-allowed; }
.page-info { color: var(--muted); font-size: 0.85rem; }
.detail-card {
  border: 1px solid var(--border); border-radius: 8px; padding: 1.5rem;
  background: var(--surface); margin-bottom: 1.5rem;
}
.detail-card h2 { font-size: 1.25rem; margin-bottom: 1rem; word-break: break-all; }
.detail-row { display: flex; margin-bottom: 0.5rem; font-size: 0.9rem; }
.detail-row .label { color: var(--muted); width: 6rem; flex-shrink: 0; }
.detail-row .value { word-break: break-all; }
.ed2k-box {
  display: flex; gap: 0.5rem; margin-top: 1rem; align-items: center;
}
.ed2k-box input {
  flex: 1; padding: 0.5rem; border: 1px solid var(--border); border-radius: 6px;
  background: var(--bg); color: var(--text); font-size: 0.8rem; font-family: monospace;
}
.peers-table { width: 100%; border-collapse: collapse; margin-top: 0.5rem; font-size: 0.85rem; }
.peers-table th, .peers-table td {
  padding: 0.5rem; text-align: left; border-bottom: 1px solid var(--border);
}
.peers-table th { color: var(--muted); font-weight: 500; }
.status-msg { text-align: center; color: var(--muted); padding: 2rem; }
.status-msg.error { color: #ef4444; }
.back-link {
  display: inline-block; color: var(--accent); text-decoration: none;
  margin-bottom: 1rem; font-size: 0.9rem;
}
.back-link:hover { text-decoration: underline; }
.lang-switch { position: absolute; top: 1rem; right: 1rem; }
.lang-switch a { color: var(--muted); text-decoration: none; font-size: 0.85rem; margin-left: 0.5rem; }
.lang-switch a.active { color: var(--accent); }
`

const publicUIScript = `(() => {
  const i18n = JSON.parse(atob(document.getElementById('public-i18n').dataset.b64));
  const token = document.getElementById('public-token')?.dataset.token || '';
  const lang = document.documentElement.lang || 'zh';

  function api(path) {
    const headers = {};
    if (token) headers['X-Public-Token'] = token;
    return fetch(path, { headers }).then(r => {
      if (!r.ok) throw new Error(i18n.Error + ' (' + r.status + ')');
      return r.json();
    }).then(body => {
      if (!body.ok) throw new Error(body.error || i18n.Error);
      return body;
    });
  }

  function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB';
    return (bytes / 1073741824).toFixed(2) + ' GB';
  }

  function ed2kLink(name, size, hash) {
    return 'ed2k://|file|' + name + '|' + size + '|' + hash + '|/';
  }

  const path = location.pathname;
  if (path.startsWith('/file/')) {
    renderDetail(path.replace('/file/', ''));
  } else {
    renderSearch();
  }

  function renderSearch() {
    const params = new URLSearchParams(location.search);
    const q = params.get('q') || '';
    const page = parseInt(params.get('page') || '1', 10);
    const sort = params.get('sort') || 'name';
    const ext = params.get('ext') || '';
    const type = params.get('type') || '';

    document.getElementById('search-input').value = q;
    document.getElementById('sort-select').value = sort;
    document.getElementById('ext-input').value = ext;
    document.getElementById('type-input').value = type;

    if (!q && path === '/') return;

    const app = document.getElementById('app');
    app.innerHTML = '<div class="status-msg">' + i18n.Loading + '</div>';

    let url = '/api/v1/search?q=' + encodeURIComponent(q) + '&page=' + page + '&sort=' + sort;
    if (ext) url += '&ext=' + encodeURIComponent(ext);
    if (type) url += '&type=' + encodeURIComponent(type);

    api(url).then(res => {
      const items = res.data || [];
      const meta = res.meta || {};
      const total = meta.total || 0;
      const perPage = meta.per_page || 50;
      const pages = Math.max(1, Math.ceil(total / perPage));

      if (items.length === 0) {
        app.innerHTML = '<div class="status-msg">' + i18n.NoResults + '</div>';
        return;
      }

      let html = '<ul class="results">';
      items.forEach(item => {
        html += '<li class="result-item" data-hash="' + item.hash + '">'
          + '<div class="name">' + esc(item.name) + '</div>'
          + '<div class="meta">'
          + '<span>' + formatSize(item.size) + '</span>'
          + '<span>' + esc(item.file_type || '') + '</span>'
          + '<span>' + i18n.SourcesLbl + ': ' + (item.sources || 0) + '</span>'
          + '</div></li>';
      });
      html += '</ul>';

      const pageInfo = i18n.PageInfo
        .replace('{page}', meta.page || page)
        .replace('{pages}', pages)
        .replace('{total}', total);

      html += '<div class="pagination">'
        + '<button id="prev-btn"' + (page <= 1 ? ' disabled' : '') + '>' + i18n.Prev + '</button>'
        + '<span class="page-info">' + pageInfo + '</span>'
        + '<button id="next-btn"' + (page >= pages ? ' disabled' : '') + '>' + i18n.Next + '</button>'
        + '</div>';

      app.innerHTML = html;

      document.querySelectorAll('.result-item').forEach(el => {
        el.addEventListener('click', () => {
          location.href = '/file/' + el.dataset.hash + '?lang=' + lang;
        });
      });
      document.getElementById('prev-btn')?.addEventListener('click', () => navigate(page - 1));
      document.getElementById('next-btn')?.addEventListener('click', () => navigate(page + 1));
    }).catch(err => {
      app.innerHTML = '<div class="status-msg error">' + esc(err.message) + '</div>';
    });
  }

  function renderDetail(hash) {
    const app = document.getElementById('app');
    app.innerHTML = '<div class="status-msg">' + i18n.Loading + '</div>';

    Promise.all([
      api('/api/v1/files/' + hash),
      api('/api/v1/files/' + hash + '/sources'),
    ]).then(([fileRes, srcRes]) => {
      const file = fileRes.data;
      const sources = srcRes.data;
      const link = ed2kLink(file.name, file.size, file.hash);

      let html = '<a class="back-link" href="/?lang=' + lang + '">&larr; ' + i18n.Back + '</a>';
      html += '<div class="detail-card"><h2>' + esc(file.name) + '</h2>';
      html += row(i18n.HashLabel, file.hash);
      html += row(i18n.SizeLabel, formatSize(file.size));
      html += row(i18n.TypeField, (file.file_type || '') + (file.extension ? ' (.' + file.extension + ')' : ''));
      html += row(i18n.SourcesLbl, sources.sources || 0);
      html += row(i18n.CompleteLbl, sources.complete || 0);
      html += '<div class="ed2k-box"><input id="ed2k-link" readonly value="' + esc(link) + '">'
        + '<button class="btn" id="copy-btn">' + i18n.CopyBtn + '</button></div>';
      html += '</div>';

      if (sources.peers && sources.peers.length > 0) {
        html += '<h3>' + i18n.PeersTitle + '</h3>';
        html += '<table class="peers-table"><thead><tr><th>Type</th><th>Host</th><th>Port</th><th>Left</th></tr></thead><tbody>';
        sources.peers.forEach(p => {
          html += '<tr><td>' + esc(p.type) + '</td><td>' + esc(p.host || '') + '</td><td>' + (p.port || '') + '</td><td>' + (p.left != null ? p.left : '-') + '</td></tr>';
        });
        html += '</tbody></table>';
      }

      app.innerHTML = html;

      document.getElementById('copy-btn')?.addEventListener('click', () => {
        const input = document.getElementById('ed2k-link');
        navigator.clipboard.writeText(input.value).then(() => {
          const btn = document.getElementById('copy-btn');
          btn.textContent = i18n.CopiedBtn;
          setTimeout(() => { btn.textContent = i18n.CopyBtn; }, 2000);
        });
      });
    }).catch(err => {
      app.innerHTML = '<div class="status-msg error">' + esc(err.message) + '</div>';
    });
  }

  function row(label, value) {
    return '<div class="detail-row"><span class="label">' + label + '</span><span class="value">' + esc(String(value)) + '</span></div>';
  }

  function esc(s) {
    const d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  function navigate(page) {
    const params = new URLSearchParams(location.search);
    params.set('page', page);
    location.search = params.toString();
  }

  document.getElementById('search-form')?.addEventListener('submit', e => {
    e.preventDefault();
    const q = document.getElementById('search-input').value.trim();
    const sort = document.getElementById('sort-select').value;
    const ext = document.getElementById('ext-input').value.trim();
    const type = document.getElementById('type-input').value.trim();
    const params = new URLSearchParams();
    if (q) params.set('q', q);
    if (sort) params.set('sort', sort);
    if (ext) params.set('ext', ext);
    if (type) params.set('type', type);
    params.set('lang', lang);
    location.href = '/search?' + params.toString();
  });
})();`
