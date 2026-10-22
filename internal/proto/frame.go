// Package proto implements the wire protocol recovered from the decompiled
// client (GameTcpClient framing + proto_mate.lua field tables).
//
// Frame:
//
//	flags:       1 byte (client writes 0)
//	body_length: 3 bytes big-endian, = 2 + payload length
//	command:     2 bytes big-endian
//	payload:     body_length - 2 bytes
package proto

import (
	"errors"
	"fmt"
	"io"
)

// Handshake is the fixed 23-byte plaintext sent by the client right after
// the TCP connection is established. It carries no frame header.
const Handshake = "game_client------------"

const (
	HeaderSize = 6
	// MaxPayload is the receive limit recovered from RecvBinaryImpl.
	MaxPayload = 0x7FFE
)

var ErrFrame = errors.New("proto: bad frame")

type Frame struct {
	Flags   byte
	Command uint16
	Payload []byte
}

func (f Frame) Encode() ([]byte, error) {
	if len(f.Payload) > MaxPayload {
		return nil, fmt.Errorf("%w: payload %d exceeds limit", ErrFrame, len(f.Payload))
	}
	n := len(f.Payload) + 2
	b := make([]byte, 0, HeaderSize+len(f.Payload))
	b = append(b, f.Flags, byte(n>>16), byte(n>>8), byte(n), byte(f.Command>>8), byte(f.Command))
	return append(b, f.Payload...), nil
}

// DecodeHeader returns flags, command and payload length.
func DecodeHeader(h []byte) (flags byte, cmd uint16, payloadLen int, err error) {
	if len(h) != HeaderSize {
		return 0, 0, 0, fmt.Errorf("%w: header must be %d bytes", ErrFrame, HeaderSize)
	}
	bodyLen := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
	if bodyLen < 2 {
		return 0, 0, 0, fmt.Errorf("%w: body_length %d < 2", ErrFrame, bodyLen)
	}
	payloadLen = bodyLen - 2
	if payloadLen > MaxPayload {
		return 0, 0, 0, fmt.Errorf("%w: payload %d exceeds limit", ErrFrame, payloadLen)
	}
	return h[0], uint16(h[4])<<8 | uint16(h[5]), payloadLen, nil
}

// ReadFrame reads exactly one frame from r.
func ReadFrame(r io.Reader) (Frame, error) {
	var h [HeaderSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Frame{}, err
	}
	flags, cmd, n, err := DecodeHeader(h[:])
	if err != nil {
		return Frame{}, err
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return Frame{}, err
	}
	return Frame{Flags: flags, Command: cmd, Payload: p}, nil
}

// ReadHandshake consumes and validates the 23-byte handshake.
func ReadHandshake(r io.Reader) error {
	b := make([]byte, len(Handshake))
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	if string(b) != Handshake {
		return fmt.Errorf("%w: bad handshake %q", ErrFrame, b)
	}
	return nil
}
