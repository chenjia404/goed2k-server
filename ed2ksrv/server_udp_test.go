package ed2ksrv

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/monkeyWie/goed2k/protocol"
	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

func TestUDPServerInfoReply(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CatalogPath = filepath.Join("..", "testdata", "catalog.json")
	cfg.AdminListenAddress = ""
	cfg.ServerName = "test-server"
	cfg.ServerDescription = "test-description"

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

	tcpAddr := listener.Addr().(*net.TCPAddr)
	udpConn := dialServerUDP(t, tcpAddr.Port+4)
	defer udpConn.Close()

	req := []byte{ed2kUDPHeader, opUDPGetServerInfo}
	if _, err := udpConn.Write(req); err != nil {
		t.Fatalf("write udp: %v", err)
	}
	resp := readUDPResponse(t, udpConn)
	if len(resp) < 4 || resp[0] != ed2kUDPHeader || resp[1] != opUDPServerInfo {
		t.Fatalf("unexpected server info response: % x", resp)
	}
}

func TestUDPSearchFileReply(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CatalogPath = filepath.Join("..", "testdata", "catalog.json")
	cfg.AdminListenAddress = ""
	cfg.SearchBatchSize = 5

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

	tcpAddr := listener.Addr().(*net.TCPAddr)
	udpConn := dialServerUDP(t, tcpAddr.Port+4)
	defer udpConn.Close()

	search := serverproto.SearchRequest{Query: "ubuntu", FileType: "Iso", Extension: "iso"}
	var body bytes.Buffer
	if err := search.Put(&body); err != nil {
		t.Fatalf("put search: %v", err)
	}
	req := append([]byte{ed2kUDPHeader, opUDPSearchFile}, body.Bytes()...)
	if _, err := udpConn.Write(req); err != nil {
		t.Fatalf("write udp search: %v", err)
	}
	resp := readUDPResponse(t, udpConn)
	if len(resp) < 2 || resp[0] != ed2kUDPHeader || resp[1] != opUDPSearchFileResults {
		t.Fatalf("unexpected udp search response: % x", resp)
	}
}

func dialServerUDP(t *testing.T, port int) net.Conn {
	t.Helper()
	target := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var (
		conn net.Conn
		err  error
	)
	for attempt := 0; attempt < 50; attempt++ {
		conn, err = net.Dial("udp", target)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		probe := []byte{ed2kUDPHeader, opGlobServStatReq, 0, 0, 0, 0}
		if _, err = conn.Write(probe); err != nil {
			_ = conn.Close()
			time.Sleep(10 * time.Millisecond)
			continue
		}
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		buf := make([]byte, 64)
		if _, err = conn.Read(buf); err == nil {
			_ = conn.SetReadDeadline(time.Time{})
			return conn
		}
		_ = conn.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("udp server not ready on %s: %v", target, err)
	return nil
}

func readUDPResponse(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read udp: %v", err)
	}
	return append([]byte(nil), buf[:n]...)
}

func TestUDPFoundSourcesReply(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CatalogPath = filepath.Join("..", "testdata", "catalog.json")
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

	record, ok := catalog.Get(mustHash(t, "31D6CFE0D16AE931B73C59D7E0C089C0"))
	if !ok {
		t.Fatal("catalog record missing")
	}
	_ = record

	tcpAddr := listener.Addr().(*net.TCPAddr)
	udpConn := dialServerUDP(t, tcpAddr.Port+4)
	defer udpConn.Close()

	hash, err := protocol.HashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	if err != nil {
		t.Fatal(err)
	}
	req := append([]byte{ed2kUDPHeader, opUDPGetSources}, hash.Bytes()...)
	if _, err := udpConn.Write(req); err != nil {
		t.Fatalf("write udp get sources: %v", err)
	}
	resp := readUDPResponse(t, udpConn)
	if len(resp) < 2 || resp[0] != ed2kUDPHeader || resp[1] != opUDPFoundSources {
		t.Fatalf("unexpected found sources response: % x", resp)
	}
	if resp[18] == 0 {
		t.Fatalf("expected at least one source")
	}
}

func mustHash(t *testing.T, value string) protocol.Hash {
	t.Helper()
	hash, err := protocol.HashFromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestGlobServStatChallengeEcho(t *testing.T) {
	resp := (&Server{cfg: DefaultConfig()}).buildGlobServStatRes(0xAABBCCDD)
	if binary.LittleEndian.Uint32(resp[2:6]) != 0xAABBCCDD {
		t.Fatalf("challenge not echoed")
	}
}

func TestBuildUDPServerInfoShortChallengePayload(t *testing.T) {
	s := &Server{cfg: DefaultConfig()}
	resp := s.buildUDPServerInfo([]byte{0xf0, 0xff})
	if len(resp) < 4 || resp[0] != ed2kUDPHeader || resp[1] != opUDPServerInfo {
		t.Fatalf("unexpected response for short challenge payload: % x", resp)
	}
}
