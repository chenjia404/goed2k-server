// Protocol obfuscation for ED2K TCP (client <-> server), compatible with
// eMule / aMule EncryptedStreamSocket "Basic Obfuscated Handshake Protocol Client <-> Server".
// Reference: amule src/EncryptedStreamSocket.cpp

package ed2ksrv

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"

	"github.com/monkeyWie/goed2k/protocol"
)

const (
	magicValueRequester = 34
	magicValueServer    = 203
	magicValueSync      = 0x835E6FC4
	enmObfuscation      = 0x00
	primeSizeBytes      = 96
	dhAgreementBits     = 128
	rc4DiscardBytes     = 1024
)

var dh768P = bytesToBig([]byte{
	0xF2, 0xBF, 0x52, 0xC5, 0x5F, 0x58, 0x7A, 0xDD, 0x53, 0x71, 0xA9, 0x36,
	0xE8, 0x86, 0xEB, 0x3C, 0x62, 0x17, 0xA3, 0x3E, 0xC3, 0x4C, 0xB4, 0x0D,
	0xC7, 0x3A, 0x41, 0xA6, 0x43, 0xAF, 0xFC, 0xE7, 0x21, 0xFC, 0x28, 0x63,
	0x66, 0x53, 0x5B, 0xDB, 0xCE, 0x25, 0x9F, 0x22, 0x86, 0xDA, 0x4A, 0x91,
	0xB2, 0x07, 0xCB, 0xAA, 0x52, 0x55, 0xD4, 0xF6, 0x1C, 0xCE, 0xAE, 0xD4,
	0x5A, 0xD5, 0xE0, 0x74, 0x7D, 0xF7, 0x78, 0x18, 0x28, 0x10, 0x5F, 0x34,
	0x0F, 0x76, 0x23, 0x87, 0xF8, 0x8B, 0x28, 0x91, 0x42, 0xFB, 0x42, 0x68,
	0x8F, 0x05, 0x15, 0x0F, 0x54, 0x8B, 0x5F, 0x43, 0x6A, 0xF7, 0x0D, 0xF3,
})

func bytesToBig(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

func encodeFixed96(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) > primeSizeBytes {
		return nil
	}
	out := make([]byte, primeSizeBytes)
	copy(out[primeSizeBytes-len(b):], b)
	return out
}

// amuleRC4 matches aMule CRC4EncryptableBuffer (RC4CreateKey + RC4Crypt).
type amuleRC4 struct {
	s    [256]byte
	x, y uint8
}

func newAmuleRC4(key []byte) *amuleRC4 {
	r := &amuleRC4{}
	for i := 0; i < 256; i++ {
		r.s[i] = uint8(i)
	}
	var index1, index2 uint8
	for i := 0; i < 256; i++ {
		index2 = uint8((int(key[index1]) + int(r.s[i]) + int(index2)) % 256)
		r.s[i], r.s[index2] = r.s[index2], r.s[i]
		index1 = uint8((int(index1) + 1) % len(key))
	}
	r.discard(rc4DiscardBytes)
	return r
}

// nextKeystreamByte matches one iteration of CRC4EncryptableBuffer::RC4Crypt (including when pachIn is NULL).
func (r *amuleRC4) nextKeystreamByte() byte {
	r.x = r.x + 1
	r.y = r.s[r.x] + r.y
	r.s[r.x], r.s[r.y] = r.s[r.y], r.s[r.x]
	idx := uint8(int(r.s[r.x]) + int(r.s[r.y]))
	return r.s[idx]
}

func (r *amuleRC4) discard(n int) {
	for i := 0; i < n; i++ {
		_ = r.nextKeystreamByte()
	}
}

func (r *amuleRC4) xorInPlace(p []byte) {
	for i := range p {
		p[i] ^= r.nextKeystreamByte()
	}
}

func md5Key97(s96 []byte, magic byte) []byte {
	buf := make([]byte, 97)
	copy(buf, s96)
	buf[96] = magic
	h := md5.Sum(buf)
	return h[:]
}

func isPlainEd2kFirstByte(b byte) bool {
	switch b {
	case protocol.EdonkeyHeader, protocol.PackedProt, protocol.EMuleProt:
		return true
	default:
		return false
	}
}

type obfConn struct {
	net.Conn
	send *amuleRC4
	recv *amuleRC4
}

func (o *obfConn) Read(p []byte) (int, error) {
	n, err := o.Conn.Read(p)
	if n > 0 && o.recv != nil {
		o.recv.xorInPlace(p[:n])
	}
	return n, err
}

func (o *obfConn) Write(p []byte) (int, error) {
	if o.send == nil {
		return o.Conn.Write(p)
	}
	buf := make([]byte, len(p))
	copy(buf, p)
	o.send.xorInPlace(buf)
	return o.Conn.Write(buf)
}

type prependConn struct {
	net.Conn
	prefix []byte
}

func (c *prependConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// serverObfuscatedHandshake runs the server-side DH + RC4 handshake (aMule-compatible).
func serverObfuscatedHandshake(conn net.Conn, firstByte byte) (net.Conn, error) {
	_ = firstByte
	gaBuf := make([]byte, 96)
	if _, err := io.ReadFull(conn, gaBuf); err != nil {
		return nil, err
	}
	padLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, padLenBuf); err != nil {
		return nil, err
	}
	padLen := int(padLenBuf[0])
	if padLen < 0 || padLen > 15 {
		return nil, fmt.Errorf("invalid obfuscation client padding length %d", padLen)
	}
	if padLen > 0 {
		if _, err := io.ReadFull(conn, make([]byte, padLen)); err != nil {
			return nil, err
		}
	}

	ga := new(big.Int).SetBytes(gaBuf)
	if ga.Sign() <= 0 || ga.Cmp(dh768P) >= 0 {
		return nil, errors.New("invalid DH G^a")
	}

	priv, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), dhAgreementBits))
	if err != nil {
		return nil, err
	}
	gb := new(big.Int).Exp(big.NewInt(2), priv, dh768P)
	S := new(big.Int).Exp(ga, priv, dh768P)
	s96 := encodeFixed96(S)
	if s96 == nil {
		return nil, errors.New("DH shared secret encoding failed")
	}

	sendKey := md5Key97(s96, magicValueServer)
	recvKey := md5Key97(s96, magicValueRequester)
	sendRC4 := newAmuleRC4(sendKey)
	recvRC4 := newAmuleRC4(recvKey)

	var rb [1]byte
	if _, err := rand.Read(rb[:]); err != nil {
		return nil, err
	}
	padN := rb[0] % 16

	tail := make([]byte, 7+int(padN))
	binary.LittleEndian.PutUint32(tail[0:4], magicValueSync)
	tail[4] = enmObfuscation
	tail[5] = enmObfuscation
	tail[6] = padN
	if padN > 0 {
		if _, err := rand.Read(tail[7:]); err != nil {
			return nil, err
		}
	}
	tailCopy := append([]byte(nil), tail...)
	sendRC4.xorInPlace(tailCopy)

	gb96 := encodeFixed96(gb)
	if gb96 == nil {
		return nil, errors.New("DH G^b encoding failed")
	}
	if _, err := conn.Write(append(gb96, tailCopy...)); err != nil {
		return nil, err
	}

	// Client -> server: encrypted B (magic + method + padlen + pad), then possibly login (same RC4 stream).
	hdr := make([]byte, 6)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, err
	}
	recvRC4.xorInPlace(hdr)
	if binary.LittleEndian.Uint32(hdr[0:4]) != magicValueSync {
		return nil, fmt.Errorf("obfuscation: bad client magic 0x%x", binary.LittleEndian.Uint32(hdr[0:4]))
	}
	if hdr[4] != enmObfuscation {
		return nil, fmt.Errorf("obfuscation: unsupported client method %d", hdr[4])
	}
	pad2 := int(hdr[5])
	if pad2 < 0 || pad2 > 15 {
		return nil, fmt.Errorf("obfuscation: bad client padding len %d", pad2)
	}
	if pad2 > 0 {
		padBuf := make([]byte, pad2)
		if _, err := io.ReadFull(conn, padBuf); err != nil {
			return nil, err
		}
		recvRC4.xorInPlace(padBuf)
	}

	// Remainder of this TCP segment may contain the first ED2K payload (e.g. login) merged with B (aMule delay-merge).
	chunk := make([]byte, 65536)
	n, err := conn.Read(chunk)
	if err != nil && n == 0 {
		return nil, err
	}
	if n > 0 {
		recvRC4.xorInPlace(chunk[:n])
	}

	var rb3 [1]byte
	if _, err := rand.Read(rb3[:]); err != nil {
		return nil, err
	}
	pad3 := int(rb3[0] % 16)
	resp := make([]byte, 6+pad3)
	binary.LittleEndian.PutUint32(resp[0:4], magicValueSync)
	resp[4] = enmObfuscation
	resp[5] = byte(pad3)
	if pad3 > 0 {
		if _, err := rand.Read(resp[6:]); err != nil {
			return nil, err
		}
	}
	respEnc := append([]byte(nil), resp...)
	sendRC4.xorInPlace(respEnc)
	if _, err := conn.Write(respEnc); err != nil {
		return nil, err
	}

	var leftover []byte
	if n > 0 {
		leftover = append([]byte(nil), chunk[:n]...)
	}

	oc := &obfConn{Conn: conn, send: sendRC4, recv: recvRC4}
	return &prependConn{Conn: oc, prefix: leftover}, nil
}
