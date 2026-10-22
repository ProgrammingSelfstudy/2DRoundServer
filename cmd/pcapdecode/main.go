// pcapdecode reads a packet capture of the original game (pcap or pcapng, e.g. the
// file the Android app PCAPdroid saves) and prints every game frame it holds,
// decoded with the same field tables as the server.
//
//	go run ./cmd/pcapdecode -in session.pcapng -out testdata/captures/original-2026-xx-xx/capture.jsonl
//
// A game TCP stream is recognised by the 23-byte handshake the client sends first.
// The tool only reads: it never connects to anything. Output is one JSON object per
// frame (time, stream, direction c2s / s2c, command, payload as hex, decoded message),
// and a table of how often each command was seen goes to stderr.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"2droundserver/internal/proto"
)

type packet struct {
	t    time.Time
	data []byte
	link uint32
}

func main() {
	in := flag.String("in", "", "capture file (pcap or pcapng)")
	out := flag.String("out", "", "JSON lines output (default: standard output)")
	flag.Parse()
	if *in == "" {
		fmt.Fprintln(os.Stderr, "usage: pcapdecode -in capture.pcapng [-out frames.jsonl]")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		fatal(err)
	}
	pkts, err := readCapture(raw)
	if err != nil {
		fatal(err)
	}
	w := io.Writer(os.Stdout)
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		w = f
	}
	n, err := decode(pkts, w, os.Stderr)
	if err != nil {
		fatal(err)
	}
	if n == 0 {
		fmt.Fprintln(os.Stderr, "no game frames found (no TCP stream starts with the game handshake)")
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "pcapdecode:", err)
	os.Exit(1)
}

// ---- capture files ----

func readCapture(b []byte) ([]packet, error) {
	if len(b) < 4 {
		return nil, fmt.Errorf("file too short")
	}
	switch binary.LittleEndian.Uint32(b) {
	case 0xa1b2c3d4, 0xa1b23c4d, 0xd4c3b2a1, 0x4d3cb2a1:
		return readPcap(b)
	case 0x0a0d0d0a:
		return readPcapng(b)
	}
	return nil, fmt.Errorf("not a pcap or pcapng file")
}

func readPcap(b []byte) ([]packet, error) {
	var bo binary.ByteOrder = binary.LittleEndian
	magic := binary.LittleEndian.Uint32(b)
	nano := false
	switch magic {
	case 0xa1b2c3d4:
	case 0xa1b23c4d:
		nano = true
	case 0xd4c3b2a1:
		bo = binary.BigEndian
	case 0x4d3cb2a1:
		bo, nano = binary.BigEndian, true
	}
	if len(b) < 24 {
		return nil, fmt.Errorf("pcap header too short")
	}
	link := bo.Uint32(b[20:])
	var out []packet
	for off := 24; off+16 <= len(b); {
		sec, frac, capLen := bo.Uint32(b[off:]), bo.Uint32(b[off+4:]), int(bo.Uint32(b[off+8:]))
		off += 16
		if off+capLen > len(b) {
			break
		}
		ns := int64(frac)
		if !nano {
			ns *= 1000
		}
		out = append(out, packet{time.Unix(int64(sec), ns), b[off : off+capLen], link})
		off += capLen
	}
	return out, nil
}

func readPcapng(b []byte) ([]packet, error) {
	var bo binary.ByteOrder = binary.LittleEndian
	type iface struct {
		link  uint32
		resol time.Duration // time of one timestamp unit
	}
	var ifaces []iface
	var out []packet
	for off := 0; off+12 <= len(b); {
		typ := binary.LittleEndian.Uint32(b[off:])
		if typ == 0x0a0d0d0a { // section header: learn the byte order
			if off+12 > len(b) {
				break
			}
			if binary.LittleEndian.Uint32(b[off+8:]) == 0x1a2b3c4d {
				bo = binary.LittleEndian
			} else {
				bo = binary.BigEndian
			}
			ifaces = nil
		}
		typ = bo.Uint32(b[off:])
		total := int(bo.Uint32(b[off+4:]))
		if total < 12 || off+total > len(b) {
			break
		}
		body := b[off+8 : off+total-4]
		switch typ {
		case 1: // interface description
			it := iface{link: uint32(bo.Uint16(body)), resol: time.Microsecond}
			for o := 8; o+4 <= len(body); { // options
				code, l := bo.Uint16(body[o:]), int(bo.Uint16(body[o+2:]))
				if code == 0 {
					break
				}
				if code == 9 && l >= 1 && o+4 < len(body) { // if_tsresol
					v := body[o+4]
					res := time.Second
					if v&0x80 == 0 {
						for i := 0; i < int(v); i++ {
							res /= 10
						}
					}
					it.resol = res
				}
				o += 4 + (l+3)/4*4
			}
			ifaces = append(ifaces, it)
		case 6: // enhanced packet
			if len(body) < 20 {
				break
			}
			id := int(bo.Uint32(body))
			ts := uint64(bo.Uint32(body[4:]))<<32 | uint64(bo.Uint32(body[8:]))
			capLen := int(bo.Uint32(body[12:]))
			if id >= len(ifaces) || 20+capLen > len(body) {
				break
			}
			t := time.Unix(0, 0).Add(time.Duration(ts) * ifaces[id].resol)
			out = append(out, packet{t, body[20 : 20+capLen], ifaces[id].link})
		}
		off += total
	}
	return out, nil
}

// ---- IP / TCP ----

type segment struct {
	seq  uint32
	t    time.Time
	data []byte
}

type flowKey struct{ src, dst string }

type stream struct {
	key  flowKey
	segs []segment
	syn  *uint32 // sequence number of the SYN, when seen
}

func ipPayload(p packet) (src, dst string, proto byte, payload []byte, ok bool) {
	d := p.data
	switch p.link {
	case 1: // ethernet
		if len(d) < 14 {
			return
		}
		et := binary.BigEndian.Uint16(d[12:])
		d = d[14:]
		for et == 0x8100 && len(d) >= 4 { // VLAN
			et = binary.BigEndian.Uint16(d[2:])
			d = d[4:]
		}
		if et != 0x0800 && et != 0x86dd {
			return
		}
	case 101, 228, 229: // raw IP
	case 113: // linux cooked
		if len(d) < 16 {
			return
		}
		d = d[16:]
	case 276: // linux cooked v2
		if len(d) < 20 {
			return
		}
		d = d[20:]
	default:
		return
	}
	if len(d) < 1 {
		return
	}
	switch d[0] >> 4 {
	case 4:
		if len(d) < 20 {
			return
		}
		ihl := int(d[0]&0xf) * 4
		total := int(binary.BigEndian.Uint16(d[2:]))
		if ihl < 20 || total < ihl || total > len(d) {
			total = len(d)
		}
		if d[9] != 6 || ihl > total {
			return
		}
		return fmt.Sprintf("%d.%d.%d.%d", d[12], d[13], d[14], d[15]), fmt.Sprintf("%d.%d.%d.%d", d[16], d[17], d[18], d[19]), 6, d[ihl:total], true
	case 6:
		if len(d) < 40 || d[6] != 6 {
			return
		}
		plen := int(binary.BigEndian.Uint16(d[4:]))
		end := 40 + plen
		if end > len(d) {
			end = len(d)
		}
		return fmt.Sprintf("%x", d[8:24]), fmt.Sprintf("%x", d[24:40]), 6, d[40:end], true
	}
	return
}

func tcpStreams(pkts []packet) map[flowKey]*stream {
	streams := map[flowKey]*stream{}
	for _, p := range pkts {
		src, dst, _, seg, ok := ipPayload(p)
		if !ok || len(seg) < 20 {
			continue
		}
		off := int(seg[12]>>4) * 4
		if off < 20 || off > len(seg) {
			continue
		}
		sp, dp := binary.BigEndian.Uint16(seg[0:]), binary.BigEndian.Uint16(seg[2:])
		k := flowKey{fmt.Sprintf("%s:%d", src, sp), fmt.Sprintf("%s:%d", dst, dp)}
		s := streams[k]
		if s == nil {
			s = &stream{key: k}
			streams[k] = s
		}
		seq := binary.BigEndian.Uint32(seg[4:])
		if seg[13]&0x02 != 0 { // SYN
			v := seq + 1
			s.syn = &v
		}
		if data := seg[off:]; len(data) > 0 {
			s.segs = append(s.segs, segment{seq, p.t, data})
		}
	}
	return streams
}

// bytes returns the contiguous byte stream and, per segment, where it starts.
func (s *stream) bytes() (data []byte, marks []mark, gap bool) {
	if len(s.segs) == 0 {
		return nil, nil, false
	}
	base := s.segs[0].seq
	if s.syn != nil {
		base = *s.syn
	} else {
		for _, sg := range s.segs {
			if int32(sg.seq-base) < 0 {
				base = sg.seq
			}
		}
	}
	segs := append([]segment(nil), s.segs...)
	sort.SliceStable(segs, func(i, j int) bool { return int32(segs[i].seq-segs[j].seq) < 0 })
	next := base
	for _, sg := range segs {
		rel := int32(sg.seq - next)
		d := sg.data
		switch {
		case rel > 0: // a hole: stop here, what follows cannot be trusted
			return data, marks, true
		case rel < 0: // retransmission, maybe with new bytes at the end
			skip := int(-rel)
			if skip >= len(d) {
				continue
			}
			d = d[skip:]
		}
		marks = append(marks, mark{len(data), sg.t})
		data = append(data, d...)
		next += uint32(len(d))
	}
	return data, marks, false
}

type mark struct {
	off int
	t   time.Time
}

func timeAt(marks []mark, off int) time.Time {
	var t time.Time
	for _, m := range marks {
		if m.off > off {
			break
		}
		t = m.t
	}
	return t
}

// ---- frames ----

type record struct {
	Time       string        `json:"t"`
	Stream     string        `json:"stream"`
	Dir        string        `json:"dir"`
	Cmd        uint16        `json:"cmd"`
	PayloadHex string        `json:"payload_hex"`
	Msg        proto.Message `json:"msg,omitempty"`
	Error      string        `json:"error,omitempty"`
}

func decode(pkts []packet, w, log io.Writer) (int, error) {
	schema, err := proto.LoadSchema()
	if err != nil {
		return 0, err
	}
	streams := tcpStreams(pkts)
	enc := json.NewEncoder(w)
	counts := map[string]map[uint16]int{"c2s": {}, "s2c": {}}
	total := 0
	var keys []flowKey
	for k := range streams {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].src+keys[i].dst < keys[j].src+keys[j].dst })
	for _, k := range keys {
		data, marks, gap := streams[k].bytes()
		if !bytes.HasPrefix(data, []byte(proto.Handshake)) {
			continue // not a client->server game stream
		}
		peer := streams[flowKey{k.dst, k.src}]
		name := k.src + " -> " + k.dst
		fmt.Fprintf(log, "game stream %s%s\n", name, map[bool]string{true: " (HOLE: some packets are missing)"}[gap])
		total += emit(enc, name, "c2s", data[len(proto.Handshake):], len(proto.Handshake), marks, schema.Send, counts["c2s"], log)
		if peer != nil {
			pd, pm, pgap := peer.bytes()
			if pgap {
				fmt.Fprintln(log, "  (the server side has a HOLE: some packets are missing)")
			}
			total += emit(enc, name, "s2c", pd, 0, pm, schema.Recv, counts["s2c"], log)
		}
	}
	for _, dir := range []string{"c2s", "s2c"} {
		var cmds []int
		for c := range counts[dir] {
			cmds = append(cmds, int(c))
		}
		sort.Ints(cmds)
		fmt.Fprintf(log, "%s commands:", dir)
		for _, c := range cmds {
			fmt.Fprintf(log, " %d:%d", c, counts[dir][uint16(c)])
		}
		fmt.Fprintln(log)
	}
	return total, nil
}

func emit(enc *json.Encoder, name, dir string, data []byte, shift int, marks []mark, fields map[uint16][]proto.Field, count map[uint16]int, log io.Writer) int {
	r := bytes.NewReader(data)
	n := 0
	for {
		pos := len(data) - r.Len()
		fr, err := proto.ReadFrame(r)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				fmt.Fprintf(log, "  %s: stopped at byte %d: %v\n", dir, pos, err)
			}
			return n
		}
		rec := record{Time: timeAt(marks, pos+shift).UTC().Format(time.RFC3339Nano), Stream: name, Dir: dir, Cmd: fr.Command, PayloadHex: hex.EncodeToString(fr.Payload)}
		if f, ok := fields[fr.Command]; ok {
			if m, err := proto.Decode(proto.DefaultOptions(), f, fr.Payload); err == nil {
				rec.Msg = m
			} else {
				rec.Error = err.Error()
			}
		} else {
			rec.Error = "no field table for this command"
		}
		enc.Encode(rec)
		count[fr.Command]++
		n++
	}
}
