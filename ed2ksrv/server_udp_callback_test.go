package ed2ksrv

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/monkeyWie/goed2k/protocol"
	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

func TestParseUDPCallbackRequestFullFormat(t *testing.T) {
	var payload bytes.Buffer
	origin := protocol.NewEndpoint(0x0A000001, 4662)
	_ = protocol.WriteEndpoint(&payload, origin)
	_ = protocol.WriteInt32(&payload, 0x12345678)

	targetID, gotOrigin, err := parseUDPCallbackRequest(payload.Bytes(), nil)
	if err != nil {
		t.Fatalf("parse callback: %v", err)
	}
	if targetID != 0x12345678 {
		t.Fatalf("unexpected target id: 0x%08x", targetID)
	}
	if gotOrigin.IP() != origin.IP() || gotOrigin.Port() != origin.Port() {
		t.Fatalf("unexpected origin: %s", gotOrigin.String())
	}
}

func TestParseUDPCallbackRequestLegacyFormat(t *testing.T) {
	requester := &clientSession{
		assignedID: 0x0A000002,
		loginPoint: protocol.NewEndpoint(0x0A000002, 4711),
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, 0x01020304)

	targetID, origin, err := parseUDPCallbackRequest(payload, requester)
	if err != nil {
		t.Fatalf("parse legacy callback: %v", err)
	}
	if targetID != 0x01020304 {
		t.Fatalf("unexpected target id: 0x%08x", targetID)
	}
	if origin.IP() != 0x0A000002 || origin.Port() != 4711 {
		t.Fatalf("unexpected origin: %s", origin.String())
	}
}

func TestParseUDPCallbackRequestRejectsShortPayload(t *testing.T) {
	if _, _, err := parseUDPCallbackRequest([]byte{1, 2, 3}, nil); err == nil {
		t.Fatal("expected short payload error")
	}
}

func TestUDPCallbackForwardsToTarget(t *testing.T) {
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

	targetConn, targetID := connectAndLogin(t, listener.Addr().String(), 4663, "target-client")
	defer targetConn.Close()

	tcpAddr := listener.Addr().(*net.TCPAddr)
	udpConn := dialServerUDP(t, tcpAddr.Port+4)
	defer udpConn.Close()

	origin := protocol.NewEndpoint(0x7F000001, 4662)
	var payload bytes.Buffer
	_ = protocol.WriteEndpoint(&payload, origin)
	_ = protocol.WriteInt32(&payload, targetID)

	req := append([]byte{ed2kUDPHeader, opUDPCallbackReq}, payload.Bytes()...)
	if _, err := udpConn.Write(req); err != nil {
		t.Fatalf("write udp callback: %v", err)
	}

	_ = targetConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	combiner := serverproto.NewPacketCombiner()
	for {
		packet, err := readPacket(targetConn, &combiner)
		if err != nil {
			t.Fatalf("read target packet: %v", err)
		}
		if incoming, ok := packet.(*serverproto.CallbackRequestIncoming); ok {
			if incoming.Point.IP() != origin.IP() || incoming.Point.Port() != origin.Port() {
				t.Fatalf("unexpected callback origin: %s", incoming.Point.String())
			}
			return
		}
	}
}

func TestUDPCallbackFailWhenTargetMissing(t *testing.T) {
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

	tcpAddr := listener.Addr().(*net.TCPAddr)
	udpConn := dialServerUDP(t, tcpAddr.Port+4)
	defer udpConn.Close()

	const missingID int32 = 0x42424242
	origin := protocol.NewEndpoint(0x7F000001, 4662)
	var payload bytes.Buffer
	_ = protocol.WriteEndpoint(&payload, origin)
	_ = protocol.WriteInt32(&payload, missingID)

	req := append([]byte{ed2kUDPHeader, opUDPCallbackReq}, payload.Bytes()...)
	if _, err := udpConn.Write(req); err != nil {
		t.Fatalf("write udp callback: %v", err)
	}

	resp := readUDPResponse(t, udpConn)
	if len(resp) < 6 || resp[0] != ed2kUDPHeader || resp[1] != opUDPCallbackFail {
		t.Fatalf("unexpected callback fail response: % x", resp)
	}
	if int32(binary.LittleEndian.Uint32(resp[2:6])) != missingID {
		t.Fatalf("unexpected callback fail client id: % x", resp[2:6])
	}
}

func connectAndLogin(t *testing.T, listenAddr string, listenPort int, name string) (net.Conn, int32) {
	t.Helper()
	conn, err := net.Dial("tcp", listenAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	combiner := serverproto.NewPacketCombiner()
	login := serverproto.NewLoginRequest(protocol.EMule, listenPort, name)
	if err := writePacket(conn, combiner, "server.LoginRequest", &login); err != nil {
		t.Fatalf("write login: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for login")
		}
		packet, err := readPacket(conn, &combiner)
		if err != nil {
			t.Fatalf("read login response: %v", err)
		}
		if id, ok := packet.(*serverproto.IdChange); ok {
			return conn, id.ClientID
		}
	}
}

func TestBuildUDPCallbackFailPacket(t *testing.T) {
	const clientID int32 = 0x01020304
	resp := buildUDPCallbackFail(clientID)
	if len(resp) != 6 || resp[0] != ed2kUDPHeader || resp[1] != opUDPCallbackFail {
		t.Fatalf("unexpected packet: % x", resp)
	}
	if int32(binary.LittleEndian.Uint32(resp[2:6])) != clientID {
		t.Fatalf("unexpected client id in fail packet")
	}
}

func TestFindClientByRemoteIP(t *testing.T) {
	server := &Server{clients: map[int32]*clientSession{
		1: {remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 40001}},
		2: {remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.2"), Port: 40002}},
	}}
	if got := server.findClientByRemoteIP(net.ParseIP("127.0.0.2")); got == nil || got.remote.Port != 40002 {
		t.Fatal("expected to find client by remote ip")
	}
	if got := server.findClientByRemoteIP(net.ParseIP("10.0.0.1")); got != nil {
		t.Fatal("expected nil for unknown ip")
	}
}

func TestForwardCallbackRequiresConnectedTarget(t *testing.T) {
	server := &Server{clients: map[int32]*clientSession{}}
	err := server.forwardCallback(42, protocol.NewEndpoint(0x7F000001, 4662))
	if err == nil {
		t.Fatal("expected error for missing target")
	}
}
