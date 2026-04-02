package ed2ksrv

import (
	"encoding/binary"
	"net"
)

// eD2k 客户端向服务器 UDP 端口发送统计请求（通常为 TCP 端口 + 4）。
// aMule ServerUDPSocket.cpp OP_GLOBSERVSTATRES：challenge、用户数、文件数、maxusers、softfiles、hardfiles。
const (
	ed2kUDPHeader       byte = 0xe3
	opGlobServStatReq   byte = 0x96
	opGlobServStatRes   byte = 0x97
	globServStatResSize      = 24 // challenge + 6×uint32
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
		default:
			// 其他 UDP 操作码暂不实现
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
