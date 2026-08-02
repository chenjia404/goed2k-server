package ed2ksrv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/monkeyWie/goed2k/protocol"
)

const (
	opUDPCallbackReq  byte = 0x9c // OP_GLOBCALLBACKREQ：<IP 4><PORT 2><client_ID 4>
	opUDPCallbackFail byte = 0x9e // OP_INVALID_LOWID / UDP callback fail：<client_ID 4>
)

// parseUDPCallbackRequest 解析 OP_GLOBCALLBACKREQ (0x9C) 载荷。
// eMule 标准格式：<IP 4><PORT 2><client_ID 4>（10 字节）。
// 兼容旧版仅含 <client_ID 4> 的格式，此时请求方端点从已登录 TCP 会话推断。
func parseUDPCallbackRequest(payload []byte, requester *clientSession) (targetID int32, origin protocol.Endpoint, err error) {
	if len(payload) >= 10 {
		reader := bytes.NewReader(payload[:10])
		ep, err := protocol.ReadEndpoint(reader)
		if err != nil {
			return 0, protocol.Endpoint{}, err
		}
		targetID, err = protocol.ReadInt32(reader)
		if err != nil {
			return 0, protocol.Endpoint{}, err
		}
		if !ep.Defined() {
			return 0, protocol.Endpoint{}, fmt.Errorf("callback origin endpoint is empty")
		}
		return targetID, ep, nil
	}
	if len(payload) >= 4 {
		targetID = int32(binary.LittleEndian.Uint32(payload[:4]))
		if requester == nil {
			return 0, protocol.Endpoint{}, fmt.Errorf("callback requester session not found")
		}
		requester.mu.Lock()
		origin = protocol.NewEndpoint(requester.assignedID, requester.loginPoint.Port())
		requester.mu.Unlock()
		if targetID == 0 || !origin.Defined() {
			return 0, protocol.Endpoint{}, fmt.Errorf("callback requester endpoint is empty")
		}
		return targetID, origin, nil
	}
	return 0, protocol.Endpoint{}, fmt.Errorf("callback payload too short: %d", len(payload))
}

func buildUDPCallbackFail(targetClientID int32) []byte {
	out := make([]byte, 2+4)
	out[0] = ed2kUDPHeader
	out[1] = opUDPCallbackFail
	binary.LittleEndian.PutUint32(out[2:], uint32(targetClientID))
	return out
}

func (s *Server) replyUDPCallback(pc *net.UDPConn, addr *net.UDPAddr, payload []byte) {
	requester := s.findClientByRemoteIP(addr.IP)
	targetID, origin, err := parseUDPCallbackRequest(payload, requester)
	if err != nil {
		s.logger.Debug("udp callback parse failed", "err", err, "remote", addr.String(), "len", len(payload))
		return
	}
	s.bumpCounter(func(stats *serverCounters) {
		stats.CallbackRequests++
	})
	if err := s.forwardCallback(targetID, origin); err != nil {
		s.logger.Debug("udp callback forward failed", "err", err, "target", targetID, "remote", addr.String())
		_, _ = pc.WriteToUDP(buildUDPCallbackFail(targetID), addr)
	}
}
