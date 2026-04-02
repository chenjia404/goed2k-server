package ed2ksrv

import (
	"bytes"

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

func reportedIPForIdChange(assignedID int32) uint32 {
	u := uint32(assignedID)
	// eMule: ASSERT(dwServerReportedIP == new_id || IsLowID(new_id))；低 ID 时可用服务器观测到的公网 IP。
	if u < 0x01000000 {
		return 0
	}
	return u
}

func obfuscationTCPPortAdvertised(cfg Config, listenerPort uint16) uint32 {
	if !cfg.ProtocolObfuscation || listenerPort == 0 {
		return 0
	}
	return uint32(listenerPort)
}
