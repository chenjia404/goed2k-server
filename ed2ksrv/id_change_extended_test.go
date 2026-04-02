package ed2ksrv

import (
	"bytes"
	"testing"

	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

func TestIdChangeExtendedPackSize(t *testing.T) {
	combiner := serverproto.NewPacketCombiner()
	ic := idChangeExtended{
		ClientID:             0x01020304,
		TCPFlags:             1,
		AuxPort:              4661,
		ReportedIP:           0x01020304,
		ObfuscationTCPPort:   4661,
	}
	raw, err := combiner.Pack("server.IdChange", &ic)
	if err != nil {
		t.Fatal(err)
	}
	// 头部 + 20 字节正文
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
