package ed2ksrv

import (
	"bytes"
	"fmt"
	"net"
	"strings"

	"github.com/monkeyWie/goed2k/protocol"
)

// eMule ServerSocket.cpp OP_IDCHANGE 扩展：在 ClientID、TCPFlags、AuxPort 之后还有
// ReportedIP(uint32)、ObfuscationTCPPort(uint32)。客户端只有读到非零混淆端口才会在服务器列表显示混淆能力。
const idChangeExtendedSize = 20

// idChangeExtended 与 eMule/aMule 解析一致，见 aMule ServerSocket.cpp case OP_IDCHANGE。
type idChangeExtended struct {
	ClientID           int32
	TCPFlags           int32
	AuxPort            int32
	ReportedIP         uint32
	ObfuscationTCPPort uint32
}

func (i *idChangeExtended) Get(src *bytes.Reader) error {
	v, err := protocol.ReadInt32(src)
	if err != nil {
		return err
	}
	i.ClientID = v
	if src.Len() >= 4 {
		v, err := protocol.ReadInt32(src)
		if err != nil {
			return err
		}
		i.TCPFlags = v
	}
	if src.Len() >= 4 {
		v, err := protocol.ReadInt32(src)
		if err != nil {
			return err
		}
		i.AuxPort = v
	}
	if src.Len() >= 4 {
		v, err := protocol.ReadUInt32(src)
		if err != nil {
			return err
		}
		i.ReportedIP = v
	}
	if src.Len() >= 4 {
		v, err := protocol.ReadUInt32(src)
		if err != nil {
			return err
		}
		i.ObfuscationTCPPort = v
	}
	return nil
}

func (i idChangeExtended) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteInt32(dst, i.ClientID); err != nil {
		return err
	}
	if err := protocol.WriteInt32(dst, i.TCPFlags); err != nil {
		return err
	}
	if err := protocol.WriteInt32(dst, i.AuxPort); err != nil {
		return err
	}
	if err := protocol.WriteUInt32(dst, i.ReportedIP); err != nil {
		return err
	}
	return protocol.WriteUInt32(dst, i.ObfuscationTCPPort)
}

func (i idChangeExtended) BytesCount() int { return idChangeExtendedSize }

func isLowClientID(assignedID int32) bool {
	return uint32(assignedID) < 0x01000000
}

func isPrivateOrLoopbackIP(ip uint32) bool {
	if ip == 0 {
		return true
	}
	b0 := byte(ip)
	b1 := byte(ip >> 8)
	if b0 == 10 {
		return true
	}
	if b0 == 172 && b1 >= 16 && b1 <= 31 {
		return true
	}
	if b0 == 192 && b1 == 168 {
		return true
	}
	if b0 == 127 {
		return true
	}
	if b0 == 169 && b1 == 254 {
		return true
	}
	return false
}

func parseReportedPublicIP(value string) (uint32, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return 0, fmt.Errorf("invalid reported_public_ip: %q", value)
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0, fmt.Errorf("reported_public_ip must be IPv4: %q", value)
	}
	packed := uint32(ip4[0]) | uint32(ip4[1])<<8 | uint32(ip4[2])<<16 | uint32(ip4[3])<<24
	if isPrivateOrLoopbackIP(packed) {
		return 0, fmt.Errorf("reported_public_ip must be a public IPv4 address: %q", value)
	}
	return packed, nil
}

func reportedIPForIdChange(assignedID int32, remoteIP uint32, configuredIP uint32) uint32 {
	if !isLowClientID(assignedID) {
		return uint32(assignedID)
	}
	if configuredIP != 0 {
		return configuredIP
	}
	if !isPrivateOrLoopbackIP(remoteIP) {
		return remoteIP
	}
	return 0
}

func obfuscationTCPPortAdvertised(cfg Config, listenerPort uint16) uint32 {
	if !cfg.ProtocolObfuscation || listenerPort == 0 {
		return 0
	}
	return uint32(listenerPort)
}
