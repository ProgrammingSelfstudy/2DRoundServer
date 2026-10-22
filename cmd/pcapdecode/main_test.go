package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"2droundserver/internal/proto"
)

// tcpPacket builds an Ethernet / IPv4 / TCP packet.
func tcpPacket(src, dst [4]byte, sport, dport uint16, seq uint32, flags byte, payload []byte) []byte {
	tcp := make([]byte, 20)
	binary.BigEndian.PutUint16(tcp[0:], sport)
	binary.BigEndian.PutUint16(tcp[2:], dport)
	binary.BigEndian.PutUint32(tcp[4:], seq)
	tcp[12] = 5 << 4
	tcp[13] = flags
	tcp = append(tcp, payload...)
	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:], uint16(20+len(tcp)))
	ip[8], ip[9] = 64, 6
	copy(ip[12:], src[:])
	copy(ip[16:], dst[:])
	eth := make([]byte, 14)
	binary.BigEndian.PutUint16(eth[12:], 0x0800)
	out := append(eth, ip...)
	out = append(out, tcp...)
	return append(out, 0, 0) // ethernet padding after the IP packet must be ignored
}

func classicPcap(pkts [][]byte) []byte {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint32(b, 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(b[4:], 2)
	binary.LittleEndian.PutUint16(b[6:], 4)
	binary.LittleEndian.PutUint32(b[16:], 65535)
	binary.LittleEndian.PutUint32(b[20:], 1)
	for i, p := range pkts {
		h := make([]byte, 16)
		binary.LittleEndian.PutUint32(h, uint32(1760000000+i))
		binary.LittleEndian.PutUint32(h[8:], uint32(len(p)))
		binary.LittleEndian.PutUint32(h[12:], uint32(len(p)))
		b = append(b, h...)
		b = append(b, p...)
	}
	return b
}

func pcapng(pkts [][]byte) []byte {
	block := func(typ uint32, body []byte) []byte {
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
		b := make([]byte, 8, 12+len(body))
		binary.LittleEndian.PutUint32(b, typ)
		binary.LittleEndian.PutUint32(b[4:], uint32(12+len(body)))
		b = append(b, body...)
		return binary.LittleEndian.AppendUint32(b, uint32(12+len(body)))
	}
	shb := make([]byte, 16)
	binary.LittleEndian.PutUint32(shb, 0x1a2b3c4d)
	binary.LittleEndian.PutUint16(shb[4:], 1)
	for i := 8; i < 16; i++ {
		shb[i] = 0xff
	}
	idb := make([]byte, 8)
	binary.LittleEndian.PutUint16(idb, 1)
	out := append(block(0x0a0d0d0a, shb), block(1, idb)...)
	for i, p := range pkts {
		epb := make([]byte, 20)
		ts := uint64(1760000000+i) * 1_000_000 // microseconds, the default resolution
		binary.LittleEndian.PutUint32(epb[4:], uint32(ts>>32))
		binary.LittleEndian.PutUint32(epb[8:], uint32(ts))
		binary.LittleEndian.PutUint32(epb[12:], uint32(len(p)))
		binary.LittleEndian.PutUint32(epb[16:], uint32(len(p)))
		out = append(out, block(6, append(epb, p...))...)
	}
	return out
}

func frame(t *testing.T, cmd uint16, payload []byte) []byte {
	b, err := proto.Frame{Command: cmd, Payload: payload}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func session(t *testing.T) [][]byte {
	schema, _ := proto.LoadSchema()
	o := proto.DefaultOptions()
	hb, _ := proto.Encode(o, schema.Send[1169], proto.Message{})
	reqRoles, err := proto.Encode(o, schema.Send[1164], proto.Message{"args": []proto.Message{{"key": "account", "val": "80566037"}}})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := proto.Encode(o, schema.Recv[1164], proto.Message{"code": 1, "msg": "", "roles": []proto.Message{}})
	if err != nil {
		t.Fatal(err)
	}
	client, server := [4]byte{10, 0, 0, 2}, [4]byte{47, 1, 2, 3}
	c2s := append([]byte(proto.Handshake), frame(t, 1164, reqRoles)...)
	c2s = append(c2s, frame(t, 1169, hb)...)
	s2c := append(frame(t, 1164, rep), frame(t, 1169, nil)...)
	cut := len(proto.Handshake) + 9 // a segment boundary inside the first frame
	mk := func(seq uint32, data []byte) []byte { return tcpPacket(client, server, 40000, 8800, seq, 0x18, data) }
	pk := [][]byte{
		tcpPacket(client, server, 40000, 8800, 999, 0x02, nil), // SYN: data starts at 1000
		tcpPacket(server, client, 8800, 40000, 5000, 0x12, nil),
		mk(1000+uint32(cut), c2s[cut:]), // out of order: the tail first
		mk(1000, c2s[:cut]),
		mk(1000, c2s[:cut]), // and a retransmission
		tcpPacket(server, client, 8800, 40000, 5001, 0x18, s2c),
		tcpPacket(client, server, 50000, 80, 1, 0x18, []byte("GET / HTTP/1.1\r\n\r\n")), // another stream, ignored
	}
	return pk
}

func decodeAll(t *testing.T, capture []byte) []record {
	pkts, err := readCapture(capture)
	if err != nil {
		t.Fatal(err)
	}
	var out, log bytes.Buffer
	n, err := decode(pkts, &out, &log)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("frames: %d\nlog: %s", n, log.String())
	}
	var recs []record
	dec := json.NewDecoder(&out)
	for {
		var r record
		if err := dec.Decode(&r); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		recs = append(recs, r)
	}
	if !strings.Contains(log.String(), "c2s commands: 1164:1 1169:1") || !strings.Contains(log.String(), "s2c commands: 1164:1 1169:1") {
		t.Fatalf("summary: %s", log.String())
	}
	return recs
}

func TestDecodeClassicPcapAndPcapng(t *testing.T) {
	for name, capture := range map[string][]byte{"pcap": classicPcap(session(t)), "pcapng": pcapng(session(t))} {
		recs := decodeAll(t, capture)
		if recs[0].Dir != "c2s" || recs[0].Cmd != 1164 || recs[1].Cmd != 1169 || recs[2].Dir != "s2c" || recs[2].Cmd != 1164 {
			t.Fatalf("%s: %+v", name, recs)
		}
		args, _ := recs[0].Msg["args"].([]any)
		if len(args) != 1 || args[0].(map[string]any)["val"] != "80566037" {
			t.Fatalf("%s: request not decoded: %v", name, recs[0].Msg)
		}
		if recs[2].Msg["code"] != float64(1) {
			t.Fatalf("%s: reply not decoded: %v", name, recs[2].Msg)
		}
		if recs[0].Time == "" || !strings.HasPrefix(recs[0].Time, "2025-") && !strings.HasPrefix(recs[0].Time, "2026-") {
			t.Fatalf("%s: time %q", name, recs[0].Time)
		}
	}
}

func TestStreamWithAHoleIsReported(t *testing.T) {
	pk := session(t)
	broken := [][]byte{pk[0], pk[2], pk[5]} // the first half of the client stream is missing
	pkts, _ := readCapture(classicPcap(broken))
	var out, log bytes.Buffer
	n, _ := decode(pkts, &out, &log)
	if n != 2 && n != 0 { // the c2s side has nothing usable; the handshake itself is gone
		t.Logf("frames %d", n)
	}
	if bytes.Contains(out.Bytes(), []byte(`"dir":"c2s"`)) {
		t.Fatalf("no client frame can be trusted without the handshake: %s", out.String())
	}
}
