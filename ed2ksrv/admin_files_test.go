package ed2ksrv

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"

	"github.com/monkeyWie/goed2k/protocol"
	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

func TestAdminFileSourceLabeling(t *testing.T) {
	catalogPath := copyCatalogToTemp(t)
	cfg := DefaultConfig()
	cfg.CatalogPath = catalogPath
	cfg.AdminListenAddress = ""
	cfg.AdminToken = "secret-token"

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
	login := serverproto.NewLoginRequest(protocol.EMule, 4662, "source-label-client")
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

	dynamicHash := protocol.MustHashFromString("BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	offered := OfferFiles{
		Entries: []serverproto.SharedFileEntry{
			{
				Hash:     dynamicHash,
				ClientID: 0,
				Port:     4662,
				Tags: protocol.TagList{
					protocol.NewStringTag(protocol.FTFilename, "dynamic-admin-test.mkv"),
					protocol.NewUInt32Tag(protocol.FTFileSize, 2048),
					protocol.NewStringTag(protocol.FTFileType, "Video"),
					protocol.NewStringTag(protocol.FTFileFormat, "mkv"),
				},
			},
		},
	}
	if err := writeCustomPacket(conn, opOfferFiles, &offered); err != nil {
		t.Fatalf("write offer files: %v", err)
	}
	if _, err := readPacket(conn, &combiner); err != nil {
		t.Fatalf("read offer status: %v", err)
	}

	baseURL := "http://" + adminListener.Addr().String()

	var files []AdminFileRecord
	getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/files?source=dynamic", nil, cfg.AdminToken), &files)
	if len(files) != 1 {
		t.Fatalf("unexpected dynamic file count: %+v", files)
	}
	if files[0].Source != FileSourceDynamic {
		t.Fatalf("unexpected dynamic source: %q", files[0].Source)
	}
	if len(files[0].OfferingClientIDs) != 1 || files[0].OfferingClientIDs[0] != assignedID {
		t.Fatalf("unexpected offering client ids: %+v", files[0].OfferingClientIDs)
	}

	var staticFiles []AdminFileRecord
	getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/files?source=static", nil, cfg.AdminToken), &staticFiles)
	if len(staticFiles) < 1 {
		t.Fatalf("expected static files, got %+v", staticFiles)
	}
	for _, file := range staticFiles {
		if file.Source != FileSourceStatic {
			t.Fatalf("unexpected static source on %s: %q", file.Hash, file.Source)
		}
	}

	var detail AdminFileRecord
	getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/files/"+dynamicHash.String(), nil, cfg.AdminToken), &detail)
	if detail.Source != FileSourceDynamic {
		t.Fatalf("unexpected file detail source: %+v", detail)
	}

	deleteReq := newAuthorizedRequest(t, http.MethodDelete, baseURL+"/api/files/"+dynamicHash.String(), nil, cfg.AdminToken)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete dynamic file: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict deleting dynamic file, got %d", deleteResp.StatusCode)
	}

	batchResult := map[string]any{}
	postJSONData(t, newAuthorizedRequest(t, http.MethodPost, baseURL+"/api/files/batch-delete", map[string]any{
		"hashes": []string{dynamicHash.String()},
	}, cfg.AdminToken), http.StatusOK, &batchResult, nil)
	skipped, ok := batchResult["skipped_dynamic"].([]any)
	if !ok || len(skipped) != 1 {
		t.Fatalf("unexpected batch delete skipped_dynamic: %+v", batchResult)
	}

	revokeResult := map[string]any{}
	revokeMeta := postJSONData(t, newAuthorizedRequest(t, http.MethodPost, baseURL+"/api/clients/"+int32String(assignedID)+"/revoke-offers", nil, cfg.AdminToken), http.StatusOK, &revokeResult, nil)
	if revokeMeta["status"] != "revoked" {
		t.Fatalf("unexpected revoke meta: %+v", revokeMeta)
	}
	if int(revokeResult["revoked_offers"].(float64)) != 1 {
		t.Fatalf("unexpected revoked count: %+v", revokeResult)
	}

	getJSON(t, newAuthorizedRequest(t, http.MethodGet, baseURL+"/api/files?source=dynamic", nil, cfg.AdminToken), &files)
	if len(files) != 0 {
		t.Fatalf("expected no dynamic files after revoke, got %+v", files)
	}
}

func TestFilterAdminFilesBySource(t *testing.T) {
	files := []AdminFileRecord{
		{FileRecord: FileRecord{Name: "a.iso"}, Source: FileSourceStatic},
		{FileRecord: FileRecord{Name: "b.mkv"}, Source: FileSourceDynamic},
	}
	req, _ := http.NewRequest(http.MethodGet, "/api/files?source=static", nil)
	filtered := filterAdminFiles(files, req)
	if len(filtered) != 1 || filtered[0].Source != FileSourceStatic {
		t.Fatalf("unexpected filtered static files: %+v", filtered)
	}
}
