package ed2ksrv

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/goed2k/protocol"
	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

func TestParseSearchRequestMatchesGoed2KEncoding(t *testing.T) {
	request := serverproto.SearchRequest{
		Query:              "ubuntu iso",
		MinSize:            1024,
		MaxSize:            8192,
		MinSources:         5,
		MinCompleteSources: 3,
		FileType:           "Iso",
		Extension:          "iso",
	}
	var buf bytes.Buffer
	if err := request.Put(&buf); err != nil {
		t.Fatalf("put search request: %v", err)
	}
	parsed, err := ParseSearchRequest(buf.Bytes())
	if err != nil {
		t.Fatalf("parse search request: %v", err)
	}
	if parsed.FileType != "Iso" || parsed.Extension != "iso" {
		t.Fatalf("unexpected tag filters: %#v", parsed)
	}
	if parsed.MinSize != 1024 || parsed.MaxSize != 8192 {
		t.Fatalf("unexpected size filters: %#v", parsed)
	}
	if parsed.MinSources != 5 || parsed.MinCompleteSources != 3 {
		t.Fatalf("unexpected source filters: %#v", parsed)
	}
	if len(parsed.Keywords) != 2 || parsed.Keywords[0] != "ubuntu" || parsed.Keywords[1] != "iso" {
		t.Fatalf("unexpected keywords: %#v", parsed.Keywords)
	}
}

func TestServerHandshakeSearchAndSources(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CatalogPath = filepath.Join("..", "testdata", "catalog.json")
	cfg.SearchBatchSize = 1
	cfg.Message = "test-message"
	cfg.AdminListenAddress = ""

	catalog, err := LoadCatalog(cfg.CatalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	server, err := NewServer(cfg, catalog, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		_ = server.Serve(listener)
	}()
	defer shutdownServer(t, server)

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	combiner := serverproto.NewPacketCombiner()
	login := serverproto.NewLoginRequest(protocol.EMule, 4662, "test-client")
	if err := writePacket(conn, combiner, "server.LoginRequest", &login); err != nil {
		t.Fatalf("write login: %v", err)
	}

	var assignedID int32
	statusSeen := false
	messageSeen := false
	deadline := time.Now().Add(2 * time.Second)
	for !(statusSeen && messageSeen && assignedID != 0) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for handshake packets")
		}
		packet, err := readPacket(conn, &combiner)
		if err != nil {
			t.Fatalf("read handshake packet: %v", err)
		}
		switch value := packet.(type) {
		case *serverproto.Status:
			statusSeen = true
			if value.FilesCount != 3 {
				t.Fatalf("unexpected files count: %d", value.FilesCount)
			}
		case *serverproto.Message:
			messageSeen = true
			if value.AsString() != "test-message" {
				t.Fatalf("unexpected message: %q", value.AsString())
			}
		case *serverproto.IdChange:
			assignedID = value.ClientID
		}
	}

	search := serverproto.SearchRequest{Query: "iso", FileType: "Iso", Extension: "iso"}
	if err := writePacket(conn, combiner, "server.SearchRequest", &search); err != nil {
		t.Fatalf("write search: %v", err)
	}

	packet, err := readPacket(conn, &combiner)
	if err != nil {
		t.Fatalf("read first search result: %v", err)
	}
	result, ok := packet.(*serverproto.SearchResult)
	if !ok {
		t.Fatalf("unexpected first search packet: %T", packet)
	}
	if len(result.Results) != 1 || !result.MoreResults {
		t.Fatalf("unexpected first search result packet: %+v", result)
	}
	if got, ok := result.Results[0].StringTag(protocol.FTFilename); !ok || got != "ubuntu-24.04-desktop-amd64.iso" {
		t.Fatalf("unexpected first result filename: %q, %t", got, ok)
	}

	if err := writePacket(conn, combiner, "server.SearchMore", &serverproto.SearchMore{}); err != nil {
		t.Fatalf("write search more: %v", err)
	}
	packet, err = readPacket(conn, &combiner)
	if err != nil {
		t.Fatalf("read second search result: %v", err)
	}
	result, ok = packet.(*serverproto.SearchResult)
	if !ok {
		t.Fatalf("unexpected second search packet: %T", packet)
	}
	if len(result.Results) != 1 || result.MoreResults {
		t.Fatalf("unexpected second search result packet: %+v", result)
	}
	if got, ok := result.Results[0].StringTag(protocol.FTFilename); !ok || got != "archlinux-2026.03.01-x86_64.iso" {
		t.Fatalf("unexpected second result filename: %q, %t", got, ok)
	}

	getSources := serverproto.GetFileSources{Hash: protocol.Terminal, LowPart: 1, HiPart: 0}
	if err := writePacket(conn, combiner, "server.GetFileSources", &getSources); err != nil {
		t.Fatalf("write get sources: %v", err)
	}
	packet, err = readPacket(conn, &combiner)
	if err != nil {
		t.Fatalf("read sources: %v", err)
	}
	sources, ok := packet.(*serverproto.FoundFileSources)
	if !ok {
		t.Fatalf("unexpected sources packet: %T", packet)
	}
	if len(sources.Sources) != 2 {
		t.Fatalf("unexpected sources count: %d", len(sources.Sources))
	}
	if assignedID == 0 {
		t.Fatalf("expected non-zero assigned id")
	}
}

func TestOfferFilesRegistersDynamicSharedEntries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CatalogPath = filepath.Join("..", "testdata", "catalog.json")
	cfg.SearchBatchSize = 10
	cfg.Message = "offer-test"
	cfg.AdminListenAddress = ""

	catalog, err := LoadCatalog(cfg.CatalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	server, err := NewServer(cfg, catalog, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() { _ = server.Serve(listener) }()
	defer shutdownServer(t, server)

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	combiner := serverproto.NewPacketCombiner()
	login := serverproto.NewLoginRequest(protocol.EMule, 4662, "offer-client")
	if err := writePacket(conn, combiner, "server.LoginRequest", &login); err != nil {
		t.Fatalf("write login: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := readPacket(conn, &combiner); err != nil {
			t.Fatalf("read login packet %d: %v", i, err)
		}
	}

	offered := OfferFiles{
		Entries: []serverproto.SharedFileEntry{
			{
				Hash:     protocol.MustHashFromString("BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"),
				ClientID: 0,
				Port:     4662,
				Tags: protocol.TagList{
					protocol.NewStringTag(protocol.FTFilename, "shared-runtime-video.mkv"),
					protocol.NewUInt32Tag(protocol.FTFileSize, 123456),
					protocol.NewStringTag(protocol.FTFileType, "Video"),
					protocol.NewStringTag(protocol.FTFileFormat, "mkv"),
				},
			},
		},
	}
	if err := writeCustomPacket(conn, opOfferFiles, &offered); err != nil {
		t.Fatalf("write offer files: %v", err)
	}
	packet, err := readPacket(conn, &combiner)
	if err != nil {
		t.Fatalf("read offer status: %v", err)
	}
	status, ok := packet.(*serverproto.Status)
	if !ok {
		t.Fatalf("unexpected offer response: %T", packet)
	}
	if status.FilesCount != 4 {
		t.Fatalf("unexpected files count after offer: %d", status.FilesCount)
	}

	search := serverproto.SearchRequest{Query: "shared runtime", FileType: "Video", Extension: "mkv"}
	if err := writePacket(conn, combiner, "server.SearchRequest", &search); err != nil {
		t.Fatalf("write search after offer: %v", err)
	}
	packet, err = readPacket(conn, &combiner)
	if err != nil {
		t.Fatalf("read search result after offer: %v", err)
	}
	result, ok := packet.(*serverproto.SearchResult)
	if !ok || len(result.Results) != 1 {
		t.Fatalf("unexpected search result after offer: %T %+v", packet, result)
	}
	if got, ok := result.Results[0].StringTag(protocol.FTFilename); !ok || got != "shared-runtime-video.mkv" {
		t.Fatalf("unexpected offered result filename: %q %t", got, ok)
	}

	getSources := serverproto.GetFileSources{Hash: protocol.MustHashFromString("BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"), LowPart: 1}
	if err := writePacket(conn, combiner, "server.GetFileSources", &getSources); err != nil {
		t.Fatalf("write get sources for offered file: %v", err)
	}
	packet, err = readPacket(conn, &combiner)
	if err != nil {
		t.Fatalf("read sources for offered file: %v", err)
	}
	sources, ok := packet.(*serverproto.FoundFileSources)
	if !ok || len(sources.Sources) != 1 || sources.Sources[0].Port() != 4662 {
		t.Fatalf("unexpected offered sources packet: %T %+v", packet, sources)
	}
}

func TestAdminFilesClientsAndStats(t *testing.T) {
	catalogPath := copyCatalogToTemp(t)
	cfg := DefaultConfig()
	cfg.CatalogPath = catalogPath
	cfg.AdminListenAddress = ""
	cfg.AdminToken = "secret-token"
	cfg.Message = "admin-test"

	catalog, err := LoadCatalog(cfg.CatalogPath)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	server, err := NewServer(cfg, catalog, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer tcpListener.Close()
	adminListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen admin: %v", err)
	}
	defer adminListener.Close()

	go func() { _ = server.Serve(tcpListener) }()
	go func() { _ = server.ServeAdmin(adminListener) }()
	defer shutdownServer(t, server)

	conn, err := net.Dial("tcp", tcpListener.Addr().String())
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	defer conn.Close()

	combiner := serverproto.NewPacketCombiner()
	login := serverproto.NewLoginRequest(protocol.EMule, 4662, "admin-client")
	if err := writePacket(conn, combiner, "server.LoginRequest", &login); err != nil {
		t.Fatalf("write login: %v", err)
	}
	var assignedID int32
	for i := 0; i < 3; i++ {
		packet, err := readPacket(conn, &combiner)
		if err != nil {
			t.Fatalf("read login packet %d: %v", i, err)
		}
		if value, ok := packet.(*serverproto.IdChange); ok {
			assignedID = value.ClientID
		}
	}
	search := serverproto.SearchRequest{Query: "ubuntu", FileType: "Iso", Extension: "iso"}
	if err := writePacket(conn, combiner, "server.SearchRequest", &search); err != nil {
		t.Fatalf("write search: %v", err)
	}
	if _, err := readPacket(conn, &combiner); err != nil {
		t.Fatalf("read search result: %v", err)
	}

	baseURL := "http://" + adminListener.Addr().String()

	assertUnauthorized(t, baseURL+"/api/stats")

	uiResponse, err := http.Get(baseURL + "/?lang=zh")
	if err != nil {
		t.Fatalf("get admin ui: %v", err)
	}
	defer uiResponse.Body.Close()
	if uiResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected ui status: %d", uiResponse.StatusCode)
	}
	uiBody, err := io.ReadAll(uiResponse.Body)
	if err != nil {
		t.Fatalf("read ui body: %v", err)
	}
	if !strings.Contains(string(uiBody), "登录管理界面") {
		t.Fatalf("unexpected ui body: %s", string(uiBody))
	}
	clientPage, err := http.Get(baseURL + "/clients/123")
	if err != nil {
		t.Fatalf("get client detail page: %v", err)
	}
	defer clientPage.Body.Close()
	if clientPage.StatusCode != http.StatusOK {
		t.Fatalf("unexpected client page status: %d", clientPage.StatusCode)
	}
	filePage, err := http.Get(baseURL + "/files/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err != nil {
		t.Fatalf("get file detail page: %v", err)
	}
	defer filePage.Body.Close()
	if filePage.StatusCode != http.StatusOK {
		t.Fatalf("unexpected file page status: %d", filePage.StatusCode)
	}

	jsResponse, err := http.Get(baseURL + "/app.js")
	if err != nil {
		t.Fatalf("get app.js: %v", err)
	}
	defer jsResponse.Body.Close()
	if jsResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected js status: %d", jsResponse.StatusCode)
	}

	cssResponse, err := http.Get(baseURL + "/app.css")
	if err != nil {
		t.Fatalf("get app.css: %v", err)
	}
	defer cssResponse.Body.Close()
	if cssResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected css status: %d", cssResponse.StatusCode)
	}

	var health map[string]any
	getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/healthz", nil, cfg.AdminToken), &health)
	if health["status"] != "ok" {
		t.Fatalf("unexpected health payload: %+v", health)
	}

	var clients []ClientSnapshot
	clientMeta := getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/clients?search=admin&page=1&per_page=10&sort=name", nil, cfg.AdminToken), &clients)
	if len(clients) != 1 || clients[0].ClientName != "admin-client" || clients[0].ClientID != assignedID {
		t.Fatalf("unexpected clients snapshot: %+v", clients)
	}
	if got := int(clientMeta["total"].(float64)); got != 1 {
		t.Fatalf("unexpected client meta: %+v", clientMeta)
	}

	var clientDetail ClientSnapshot
	getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/clients/"+int32String(assignedID), nil, cfg.AdminToken), &clientDetail)
	if clientDetail.ClientID != assignedID {
		t.Fatalf("unexpected client detail: %+v", clientDetail)
	}

	newFile := FileRecord{
		Hash:      protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
		Name:      "runtime-added-demo.mp3",
		Size:      4096,
		FileType:  "Audio",
		Extension: "mp3",
		Endpoints: []SourceEntry{{Host: "127.0.0.9", Port: 4662}},
	}
	postJSON(t, newAuthorizedRequest(t, http.MethodPost, baseURL+"/api/files", newFile, cfg.AdminToken), http.StatusCreated, nil)
	secondFile := FileRecord{
		Hash:      protocol.MustHashFromString("CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"),
		Name:      "runtime-added-demo-2.mp3",
		Size:      8192,
		FileType:  "Audio",
		Extension: "mp3",
		Endpoints: []SourceEntry{{Host: "127.0.0.10", Port: 4662}},
	}
	postJSON(t, newAuthorizedRequest(t, http.MethodPost, baseURL+"/api/files", secondFile, cfg.AdminToken), http.StatusCreated, nil)

	var files []FileRecord
	fileMeta := getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/files?file_type=Audio&extension=mp3&search=runtime&sort=size&page=1&per_page=1", nil, cfg.AdminToken), &files)
	if len(files) != 1 || files[0].Hash.String() != secondFile.Hash.String() {
		t.Fatalf("unexpected filtered files: %+v", files)
	}
	if got := int(fileMeta["total"].(float64)); got != 2 {
		t.Fatalf("unexpected file meta: %+v", fileMeta)
	}

	var fileDetail FileRecord
	getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/files/"+newFile.Hash.String(), nil, cfg.AdminToken), &fileDetail)
	if fileDetail.Name != newFile.Name {
		t.Fatalf("unexpected file detail: %+v", fileDetail)
	}

	batchDeleteMeta := map[string]any{}
	postJSON(t, newAuthorizedRequest(t, http.MethodPost, baseURL+"/api/files/batch-delete", map[string]any{
		"hashes": []string{newFile.Hash.String(), secondFile.Hash.String()},
	}, cfg.AdminToken), http.StatusOK, &batchDeleteMeta)
	if batchDeleteMeta["status"] != "batch_deleted" {
		t.Fatalf("unexpected batch delete meta: %+v", batchDeleteMeta)
	}

	persistMeta := map[string]any{}
	postJSON(t, newAuthorizedRequest(t, http.MethodPost, baseURL+"/api/persist", map[string]string{"op": "persist"}, cfg.AdminToken), http.StatusOK, &persistMeta)
	if persistMeta["status"] != "persisted" {
		t.Fatalf("unexpected persist meta: %+v", persistMeta)
	}

	var stats ServerStats
	statsMeta := getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/stats", nil, cfg.AdminToken), &stats)
	if stats.CurrentClients != 1 || stats.CurrentFiles != 3 {
		t.Fatalf("unexpected stats snapshot: %+v", stats)
	}
	if stats.SearchRequests < 1 || stats.FilesRegistered < 2 || stats.FilesRemoved < 2 || stats.PersistWrites < 2 {
		t.Fatalf("unexpected counters: %+v", stats)
	}
	if statsMeta["catalog_path"] != catalogPath {
		t.Fatalf("unexpected stats meta: %+v", statsMeta)
	}

	var audit []AuditEntry
	auditMeta := getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/audit?page=1&per_page=10", nil, cfg.AdminToken), &audit)
	if len(audit) < 3 {
		t.Fatalf("unexpected audit log length: %+v", audit)
	}
	if got := int(auditMeta["count"].(float64)); got < 3 {
		t.Fatalf("unexpected audit meta: %+v", auditMeta)
	}

	content, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read persisted catalog: %v", err)
	}
	if strings.Contains(string(content), newFile.Hash.String()) {
		t.Fatalf("deleted file still present in persisted catalog")
	}
	if strings.Contains(string(content), secondFile.Hash.String()) {
		t.Fatalf("batch deleted file still present in persisted catalog")
	}
}

func shutdownServer(t *testing.T, server *Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown server: %v", err)
	}
}

func copyCatalogToTemp(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "catalog.json"))
	if err != nil {
		t.Fatalf("read source catalog: %v", err)
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp catalog: %v", err)
	}
	return path
}

func writePacket(conn net.Conn, combiner protocol.PacketCombiner, typeName string, packet protocol.Serializable) error {
	raw, err := combiner.Pack(typeName, packet)
	if err != nil {
		return err
	}
	_, err = conn.Write(raw)
	return err
}

func writeCustomPacket(conn net.Conn, opcode byte, packet protocol.Serializable) error {
	var body bytes.Buffer
	if err := packet.Put(&body); err != nil {
		return err
	}
	header := protocol.PacketHeader{
		Protocol: protocol.EdonkeyHeader,
		Size:     int32(body.Len() + 1),
		Packet:   opcode,
	}
	var frame bytes.Buffer
	if err := header.Put(&frame); err != nil {
		return err
	}
	if _, err := frame.Write(body.Bytes()); err != nil {
		return err
	}
	_, err := conn.Write(frame.Bytes())
	return err
}

func readPacket(conn net.Conn, combiner *protocol.PacketCombiner) (protocol.Serializable, error) {
	header, body, _, err := readFrame(conn)
	if err != nil {
		return nil, err
	}
	return combiner.Unpack(header, body)
}

func newAuthorizedRequest(t *testing.T, method, url string, payload any, token string) *http.Request {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload for %s: %v", url, err)
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, url, err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("X-Admin-Token", token)
	}
	return request
}

func getJSON[T any](t *testing.T, request *http.Request, out *T) map[string]any {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", request.URL.String(), err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected status %d for %s: %s", response.StatusCode, request.URL.String(), string(body))
	}
	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
		Meta map[string]any  `json:"meta"`
		Err  string          `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope %s: %v", request.URL.String(), err)
	}
	if !envelope.OK {
		t.Fatalf("unexpected error envelope for %s: %s", request.URL.String(), envelope.Err)
	}
	if out != nil && envelope.Data != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			t.Fatalf("decode data %s: %v", request.URL.String(), err)
		}
	}
	return envelope.Meta
}

func postJSON(t *testing.T, request *http.Request, wantStatus int, metaOut *map[string]any) {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request %s: %v", request.URL.String(), err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected status %d for %s: %s", response.StatusCode, request.URL.String(), string(raw))
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Meta map[string]any `json:"meta"`
		Err  string         `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope %s: %v", request.URL.String(), err)
	}
	if !envelope.OK {
		t.Fatalf("unexpected error envelope for %s: %s", request.URL.String(), envelope.Err)
	}
	if metaOut != nil {
		*metaOut = envelope.Meta
	}
}

func assertUnauthorized(t *testing.T, url string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new unauthorized request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("unauthorized request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("expected 401, got %d: %s", response.StatusCode, string(body))
	}
}

func int32String(value int32) string {
	return strconv.FormatInt(int64(value), 10)
}
