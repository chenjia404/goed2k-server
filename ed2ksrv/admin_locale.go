package ed2ksrv

import (
	"net/http"
	"strings"
)

func resolveAdminLocale(r *http.Request) string {
	if q := strings.TrimSpace(r.URL.Query().Get("lang")); q != "" {
		switch strings.ToLower(q) {
		case "en":
			return "en"
		case "zh", "cn":
			return "zh"
		}
	}
	al := strings.ToLower(r.Header.Get("Accept-Language"))
	if strings.Contains(al, "zh") {
		return "zh"
	}
	return "en"
}

type adminHTMLStrings struct {
	LangAttr           string
	TitleSuffix        string
	Eyebrow            string
	BtnRefresh         string
	BtnLogout          string
	LoginKicker        string
	LoginTitle         string
	LoginHelp          string
	LabelToken         string
	PlaceholderToken   string
	BtnLogin           string
	StatClients        string
	StatConnSmall      string
	StatFiles          string
	StatRegSmall       string
	StatSearches       string
	StatEntriesSmall   string
	StatTraffic        string
	StatPacketsSmall   string
	ClientKicker       string
	ClientTitle        string
	ClientSearchL      string
	ClientSearchPh     string
	ThID               string
	ThName             string
	ThRemote           string
	ThListen           string
	ThLastSeen         string
	PagerPrev          string
	PagerNext          string
	PagerPageOne       string
	FilesKicker        string
	FilesTitle         string
	FileSearchL        string
	FileSearchPh       string
	FileTypeL          string
	FileTypePh         string
	BtnBatchDelete     string
	ThCheck            string
	ThFName            string
	ThFType            string
	ThSize             string
	ThSources          string
	ThActions          string
	RegKicker          string
	RegTitle           string
	LHash              string
	PHHash             string
	LName              string
	LSize              string
	LFileType          string
	LExt               string
	LHost              string
	LPort              string
	PHPort             string
	BtnSave            string
	InspKicker         string
	InspTitle          string
	DetailPlaceholder  string
	AuditKicker        string
	AuditTitle         string
	ThATime            string
	ThAAction          string
	ThARes             string
	ThAID              string
	ThASrc             string
	ThAStatus          string
	LangEN             string
	LangZH             string
}

type adminJSStrings struct {
	StatConnectionsFmt   string `json:"statConnectionsFmt"`
	StatRegisteredFmt    string `json:"statRegisteredFmt"`
	StatSearchEntriesFmt string `json:"statSearchEntriesFmt"`
	StatPacketsFmt       string `json:"statPacketsFmt"`
	PagerFmt             string `json:"pagerFmt"`
	EmptyClients         string `json:"emptyClients"`
	EmptyFiles           string `json:"emptyFiles"`
	EmptyAudit           string `json:"emptyAudit"`
	BtnDelete            string `json:"btnDelete"`
	ToastDeleted         string `json:"toastDeleted"`
	ToastNoSelection     string `json:"toastNoSelection"`
	ToastBatchDone       string `json:"toastBatchDone"`
	ToastInvalidToken    string `json:"toastInvalidToken"`
	ToastSaved           string `json:"toastSaved"`
	ToastLoggedOut       string `json:"toastLoggedOut"`
	InvalidResponse      string `json:"invalidResponse"`
	DateLocale           string `json:"dateLocale"`
}

var adminLocales = map[string]struct {
	HTML adminHTMLStrings
	JS   adminJSStrings
}{
	"en": {
		HTML: adminHTMLStrings{
			LangAttr:          "en",
			TitleSuffix:       "Control",
			Eyebrow:           "eD2k Server Console",
			BtnRefresh:        "Refresh",
			BtnLogout:         "Logout",
			LoginKicker:       "Admin Access",
			LoginTitle:        "Sign in",
			LoginHelp:         "Enter the token that matches X-Admin-Token. The page loads stats, clients, shared files, and audit logs via the admin API.",
			LabelToken:        "Admin Token",
			PlaceholderToken:  "Enter admin token",
			BtnLogin:          "Login",
			StatClients:       "Connected clients",
			StatConnSmall:     "Total connections 0",
			StatFiles:         "Shared files",
			StatRegSmall:      "Registered 0 / Removed 0",
			StatSearches:      "Search requests",
			StatEntriesSmall:  "Result entries 0",
			StatTraffic:       "Network traffic",
			StatPacketsSmall:  "In 0 / Out 0",
			ClientKicker:      "Clients",
			ClientTitle:       "Online clients",
			ClientSearchL:     "Search",
			ClientSearchPh:    "Name, address, hash",
			ThID:              "ID",
			ThName:            "Name",
			ThRemote:          "Remote address",
			ThListen:          "Listen endpoint",
			ThLastSeen:        "Last seen",
			PagerPrev:         "Previous",
			PagerNext:         "Next",
			PagerPageOne:      "Page 1",
			FilesKicker:       "Files",
			FilesTitle:        "Shared files",
			FileSearchL:       "Search",
			FileSearchPh:      "File name, hash",
			FileTypeL:         "Type",
			FileTypePh:        "Audio / Video / Iso",
			BtnBatchDelete:    "Delete selected",
			ThCheck:           "",
			ThFName:           "File name",
			ThFType:           "Type",
			ThSize:            "Size",
			ThSources:         "Sources",
			ThActions:         "Actions",
			RegKicker:         "Register",
			RegTitle:          "Add shared file",
			LHash:             "Hash",
			PHHash:            "32-char ED2K hash",
			LName:             "File name",
			LSize:             "Size",
			LFileType:         "Type",
			LExt:              "Extension",
			LHost:             "Source host",
			LPort:             "Source port",
			PHPort:            "4662",
			BtnSave:           "Save shared file",
			InspKicker:        "Inspect",
			InspTitle:         "Detail",
			DetailPlaceholder: "Select a client or file to show full JSON here.",
			AuditKicker:       "Audit",
			AuditTitle:        "Audit log",
			ThATime:           "Time",
			ThAAction:         "Action",
			ThARes:            "Resource",
			ThAID:             "ID",
			ThASrc:            "Source",
			ThAStatus:         "Status",
			LangEN:            "English",
			LangZH:            "中文",
		},
		JS: adminJSStrings{
			StatConnectionsFmt:   "Total connections {n}",
			StatRegisteredFmt:    "Registered {reg} / Removed {rem}",
			StatSearchEntriesFmt: "Result entries {n}",
			StatPacketsFmt:       "In {inPkts} / Out {outPkts}",
			PagerFmt:             "Page {cur} / {total}",
			EmptyClients:         "No online clients",
			EmptyFiles:           "No shared files",
			EmptyAudit:           "No audit entries",
			BtnDelete:            "Delete",
			ToastDeleted:         "Shared file deleted",
			ToastNoSelection:     "No shared files selected",
			ToastBatchDone:       "Batch delete completed",
			ToastInvalidToken:    "Invalid token, please sign in again",
			ToastSaved:           "Shared file saved",
			ToastLoggedOut:       "Signed out",
			InvalidResponse:      "invalid response",
			DateLocale:           "en-US",
		},
	},
	"zh": {
		HTML: adminHTMLStrings{
			LangAttr:          "zh-CN",
			TitleSuffix:       "管理控制台",
			Eyebrow:           "eD2k 服务器控制台",
			BtnRefresh:        "刷新",
			BtnLogout:         "退出",
			LoginKicker:       "管理访问",
			LoginTitle:        "登录管理界面",
			LoginHelp:         "输入 X-Admin-Token 对应的令牌后，页面会通过管理 API 拉取统计、客户端、共享文件和审计日志。",
			LabelToken:        "Admin Token",
			PlaceholderToken:  "输入管理令牌",
			BtnLogin:          "登录",
			StatClients:       "当前客户端",
			StatConnSmall:     "总连接 0",
			StatFiles:         "共享文件",
			StatRegSmall:      "注册 0 / 移除 0",
			StatSearches:      "搜索请求",
			StatEntriesSmall:  "结果项 0",
			StatTraffic:       "网络流量",
			StatPacketsSmall:  "入 0 / 出 0",
			ClientKicker:      "Clients",
			ClientTitle:       "在线客户端",
			ClientSearchL:     "搜索",
			ClientSearchPh:    "名称、地址、Hash",
			ThID:              "ID",
			ThName:            "名称",
			ThRemote:          "远端地址",
			ThListen:          "监听端点",
			ThLastSeen:        "最后活跃",
			PagerPrev:         "上一页",
			PagerNext:         "下一页",
			PagerPageOne:      "第 1 页",
			FilesKicker:       "Files",
			FilesTitle:        "共享文件",
			FileSearchL:       "搜索",
			FileSearchPh:      "文件名、Hash",
			FileTypeL:         "类型",
			FileTypePh:        "Audio / Video / Iso",
			BtnBatchDelete:    "批量删除",
			ThCheck:           "",
			ThFName:           "文件名",
			ThFType:           "类型",
			ThSize:            "大小",
			ThSources:         "来源",
			ThActions:         "操作",
			RegKicker:         "Register",
			RegTitle:          "新增共享文件",
			LHash:             "Hash",
			PHHash:            "32 位 ED2K Hash",
			LName:             "文件名",
			LSize:             "大小",
			LFileType:         "类型",
			LExt:              "扩展名",
			LHost:             "来源主机",
			LPort:             "来源端口",
			PHPort:            "4662",
			BtnSave:           "保存共享文件",
			InspKicker:        "Inspect",
			InspTitle:         "详情",
			DetailPlaceholder: "选择客户端或文件后，这里显示完整 JSON。",
			AuditKicker:       "Audit",
			AuditTitle:        "操作审计日志",
			ThATime:           "时间",
			ThAAction:         "动作",
			ThARes:            "资源",
			ThAID:             "标识",
			ThASrc:            "来源",
			ThAStatus:         "状态",
			LangEN:            "English",
			LangZH:            "中文",
		},
		JS: adminJSStrings{
			StatConnectionsFmt:   "总连接 {n}",
			StatRegisteredFmt:      "注册 {reg} / 移除 {rem}",
			StatSearchEntriesFmt:   "结果项 {n}",
			StatPacketsFmt:         "入 {inPkts} / 出 {outPkts}",
			PagerFmt:               "第 {cur} / {total} 页",
			EmptyClients:           "没有在线客户端",
			EmptyFiles:             "没有共享文件",
			EmptyAudit:             "暂无审计日志",
			BtnDelete:              "删除",
			ToastDeleted:           "共享文件已删除",
			ToastNoSelection:       "没有选中的共享文件",
			ToastBatchDone:         "批量删除完成",
			ToastInvalidToken:      "令牌无效，请重新登录",
			ToastSaved:             "共享文件已保存",
			ToastLoggedOut:         "已退出登录",
			InvalidResponse:        "invalid response",
			DateLocale:             "zh-CN",
		},
	},
}

func getAdminLocalePack(lang string) (adminHTMLStrings, adminJSStrings) {
	pack, ok := adminLocales[lang]
	if !ok {
		pack = adminLocales["en"]
	}
	return pack.HTML, pack.JS
}
