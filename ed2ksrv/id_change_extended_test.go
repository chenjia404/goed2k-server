package ed2ksrv

import (
	"bytes"
	"testing"

	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

func TestIdChangeExtendedPackSize(t *testing.T) {
	combiner := serverproto.NewPacketCombiner()
	ic := idChangeExtended{
		ClientID:           0x01020304,
		TCPFlags:           1,
		AuxPort:            4661,
		ReportedIP:         0x01020304,
		ObfuscationTCPPort: 4661,
	}
	raw, err := combiner.Pack("server.IdChange", &ic)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < idChangeExtendedSize {
		t.Fatalf("packet too short: %d", len(raw))
	}
	body := raw[len(raw)-idChangeExtendedSize:]
	var got idChangeExtended
	if err := got.Get(bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if got != ic {
		t.Fatalf("roundtrip: %+v vs %+v", got, ic)
	}
}

func TestReportedIPForIdChangeHighID(t *testing.T) {
	const highID int32 = 0x0A000001
	got := reportedIPForIdChange(highID, 0, 0)
	if got != uint32(highID) {
		t.Fatalf("expected high ID echoed, got 0x%08x", got)
	}
}

func TestReportedIPForIdChangeLowIDUsesConfiguredIP(t *testing.T) {
	got := reportedIPForIdChange(12345, 0, 0x01020304)
	if got != 0x01020304 {
		t.Fatalf("expected configured IP, got 0x%08x", got)
	}
}

func TestReportedIPForIdChangeLowIDUsesPublicRemoteIP(t *testing.T) {
	got := reportedIPForIdChange(12345, 0x0A000001, 0)
	if got != 0x0A000001 {
		t.Fatalf("expected remote public IP, got 0x%08x", got)
	}
}

func TestReportedIPForIdChangeLowIDSkipsPrivateRemoteIP(t *testing.T) {
	got := reportedIPForIdChange(12345, 0x0001A8C0, 0) // 192.168.0.1
	if got != 0 {
		t.Fatalf("expected zero for private remote IP, got 0x%08x", got)
	}
}

func TestParseReportedPublicIP(t *testing.T) {
	ip, err := parseReportedPublicIP("203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	if ip != 0x0a7100cb {
		t.Fatalf("unexpected packed IP: 0x%08x", ip)
	}
}

func TestConfigNormalizeReportedPublicIP(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CatalogPath = "catalog.json"
	cfg.ReportedPublicIP = "203.0.113.10"
	normalized, err := cfg.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if normalized.reportedPublicIP != 0x0a7100cb {
		t.Fatalf("unexpected normalized IP: 0x%08x", normalized.reportedPublicIP)
	}
}
