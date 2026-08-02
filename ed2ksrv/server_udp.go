package ed2ksrv

import (
	"bytes"
	"encoding/binary"
	"net"

	"github.com/monkeyWie/goed2k/protocol"
	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

// eD2k 客户端向服务器 UDP 端口发送统计请求（通常为 TCP 端口 + 4）。
// aMule ServerUDPSocket.cpp OP_GLOBSERVSTATRES：challenge、用户数、文件数、maxusers、softfiles、hardfiles。
const (
	ed2kUDPHeader            byte = 0xe3
	opGlobServStatReq        byte = 0x96
	opGlobServStatRes        byte = 0x97
	opUDPSearchFile          byte = 0x98
	opUDPSearchFileResults   byte = 0x99
	opUDPGetSources          byte = 0x9a
	opUDPFoundSources        byte = 0x9b
	opUDPServerList          byte = 0xa1
	opUDPGetServerInfo       byte = 0xa2
	opUDPServerInfo          byte = 0xa3
	opUDPGetServerList       byte = 0xa4
	globServStatResSize           = 24 // challenge + 6×uint32
	ed2kServerInfoChallenge       = 0xfff0
)

func (s *Server) maybeStartServerUDP() {
	if !s.cfg.ServerUDP {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.udpConn != nil || s.listener == nil {
		return
	}
	tcpAddr, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return
	}
	off := s.cfg.UDPPortOffset
	if off <= 0 {
		off = 4
	}
	port := tcpAddr.Port + off
	if port <= 0 || port > 65535 {
		s.logger.Warn("server UDP: invalid derived port", "tcp_port", tcpAddr.Port, "offset", off)
		return
	}
	udpAddr := &net.UDPAddr{IP: tcpAddr.IP, Port: port}
	pc, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		s.logger.Warn("server UDP listener failed (soft/hard file limits stay unknown to clients)", "err", err, "addr", udpAddr.String())
		return
	}
	s.udpConn = pc
	s.logger.Info("eD2k server UDP listening", "addr", pc.LocalAddr().String())
	go s.serveUDP()
}

func (s *Server) serveUDP() {
	s.mu.RLock()
	pc := s.udpConn
	s.mu.RUnlock()
	if pc == nil {
		return
	}
	buf := make([]byte, 2048)
	for {
		n, addr, err := pc.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			return
		}
		if n < 2 {
			continue
		}
		if buf[0] != ed2kUDPHeader {
			continue
		}
		payload := buf[2:n]
		switch buf[1] {
		case opGlobServStatReq:
			if n < 2+4 {
				continue
			}
			challenge := binary.LittleEndian.Uint32(buf[2:6])
			resp := s.buildGlobServStatRes(challenge)
			if _, werr := pc.WriteToUDP(resp, addr); werr != nil {
				s.logger.Debug("udp write", "err", werr)
			}
		case opUDPGetServerInfo:
			if resp := s.buildUDPServerInfo(payload); resp != nil {
				_, _ = pc.WriteToUDP(resp, addr)
			}
		case opUDPGetServerList:
			if resp := s.buildUDPServerList(); resp != nil {
				_, _ = pc.WriteToUDP(resp, addr)
			}
		case opUDPSearchFile:
			s.replyUDPSearchFile(pc, addr, payload)
		case opUDPGetSources:
			s.replyUDPFoundSources(pc, addr, payload)
		default:
			s.logger.Debug("unsupported udp opcode", "opcode", buf[1], "remote", addr.String())
		}
	}
}

func (s *Server) buildGlobServStatRes(challenge uint32) []byte {
	out := make([]byte, 2+globServStatResSize)
	out[0] = ed2kUDPHeader
	out[1] = opGlobServStatRes
	payload := out[2:]
	binary.LittleEndian.PutUint32(payload[0:4], challenge)
	binary.LittleEndian.PutUint32(payload[4:8], uint32(s.clientCount()))
	binary.LittleEndian.PutUint32(payload[8:12], uint32(s.currentFilesCount()))
	binary.LittleEndian.PutUint32(payload[12:16], s.cfg.MaxUsersAdvertised)
	binary.LittleEndian.PutUint32(payload[16:20], uint32(s.cfg.SoftFilesLimit))
	binary.LittleEndian.PutUint32(payload[20:24], uint32(s.cfg.HardFilesLimit))
	return out
}

func (s *Server) buildUDPServerInfo(payload []byte) []byte {
	useChallenge := len(payload) >= 2 && binary.LittleEndian.Uint16(payload[0:2]) == ed2kServerInfoChallenge
	var buf bytes.Buffer
	buf.WriteByte(ed2kUDPHeader)
	buf.WriteByte(opUDPServerInfo)
	if useChallenge {
		_, _ = buf.Write(payload[:4])
	}
	writeED2KString(&buf, s.cfg.ServerName)
	writeED2KString(&buf, s.cfg.ServerDescription)
	return buf.Bytes()
}

func (s *Server) buildUDPServerList() []byte {
	var buf bytes.Buffer
	buf.WriteByte(ed2kUDPHeader)
	buf.WriteByte(opUDPServerList)
	buf.WriteByte(0)
	return buf.Bytes()
}

func (s *Server) replyUDPSearchFile(pc *net.UDPConn, addr *net.UDPAddr, payload []byte) {
	query, err := ParseSearchRequest(payload)
	if err != nil {
		s.logger.Debug("udp search parse failed", "err", err, "remote", addr.String())
		return
	}
	results := s.searchAll(query)
	limit := s.cfg.SearchBatchSize
	if limit <= 0 {
		limit = 200
	}
	if len(results) > limit {
		results = results[:limit]
	}
	for _, entry := range results {
		packet, err := encodeUDPSharedFileEntry(entry)
		if err != nil {
			continue
		}
		_, _ = pc.WriteToUDP(packet, addr)
	}
}

func (s *Server) replyUDPFoundSources(pc *net.UDPConn, addr *net.UDPAddr, payload []byte) {
	if len(payload) < 16 {
		return
	}
	hash, err := protocol.HashFromBytes(payload[:16])
	if err != nil {
		return
	}
	sources := s.sourcesAll(hash)
	if len(sources) > 255 {
		sources = sources[:255]
	}
	var buf bytes.Buffer
	buf.WriteByte(ed2kUDPHeader)
	buf.WriteByte(opUDPFoundSources)
	_, _ = buf.Write(hash.Bytes())
	buf.WriteByte(byte(len(sources)))
	for _, endpoint := range sources {
		_ = protocol.WriteEndpoint(&buf, endpoint)
	}
	_, _ = pc.WriteToUDP(buf.Bytes(), addr)
}

func encodeUDPSharedFileEntry(entry serverproto.SharedFileEntry) ([]byte, error) {
	var body bytes.Buffer
	if err := entry.Put(&body); err != nil {
		return nil, err
	}
	packet := make([]byte, 2+body.Len())
	packet[0] = ed2kUDPHeader
	packet[1] = opUDPSearchFileResults
	copy(packet[2:], body.Bytes())
	return packet, nil
}

func writeED2KString(buf *bytes.Buffer, value string) {
	raw := []byte(value)
	_ = protocol.WriteUInt16(buf, uint16(len(raw)))
	_, _ = buf.Write(raw)
}
