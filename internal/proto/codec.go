package proto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Options describes the parts of the field encoding that the recovered Lua does
// not state: integer byte order and the width of string/array length prefixes
// (decided by the native PackData/unPackData).
type Options struct {
	ByteOrder   binary.ByteOrder
	StrLenBytes int  // 1, 2 or 4
	ArrLenBytes int  // 1, 2 or 4
	StrNullTerm bool // string bytes followed by a 0 byte, not counted in length
}

// DefaultOptions are confirmed on a real device in both directions
// (testdata/captures/wire-probe-2026-10-18): big-endian integers, 2-byte
// length prefix for strings and arrays, no NUL terminator.
func DefaultOptions() Options {
	return Options{ByteOrder: binary.BigEndian, StrLenBytes: 2, ArrLenBytes: 2}
}

func (o Options) validate() error {
	ok := func(n int) bool { return n == 1 || n == 2 || n == 4 }
	if o.ByteOrder == nil || !ok(o.StrLenBytes) || !ok(o.ArrLenBytes) {
		return errors.New("proto: invalid codec options")
	}
	return nil
}

// Message is a decoded payload. Integers are int64 (uint64 values above
// MaxInt64 are not expected in this protocol), strings are string, arrays are
// []Message.
type Message map[string]any

func (m Message) Int(name string) int64 {
	switch v := m[name].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func (m Message) Str(name string) string { s, _ := m[name].(string); return s }

func (m Message) Arr(name string) []Message { a, _ := m[name].([]Message); return a }

var ErrDecode = errors.New("proto: decode")

type writer struct {
	b []byte
	o Options
}

func (w *writer) uint(n int, v uint64) {
	var t [8]byte
	switch n {
	case 1:
		w.b = append(w.b, byte(v))
	case 2:
		w.o.ByteOrder.PutUint16(t[:2], uint16(v))
		w.b = append(w.b, t[:2]...)
	case 4:
		w.o.ByteOrder.PutUint32(t[:4], uint32(v))
		w.b = append(w.b, t[:4]...)
	case 8:
		w.o.ByteOrder.PutUint64(t[:8], v)
		w.b = append(w.b, t[:8]...)
	}
}

func toInt(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		if x > math.MaxInt64 {
			return 0, errors.New("uint64 overflow")
		}
		return int64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("not an integer: %T", v)
}

func intSize(t string) int {
	switch t {
	case "int8", "uint8", "byte":
		return 1
	case "int16", "uint16":
		return 2
	case "int32", "uint32":
		return 4
	case "uint64":
		return 8
	}
	return 0
}

// Encode serialises msg according to fields. Missing integer fields encode as
// 0 and missing strings as ""; unknown keys in msg are ignored.
func Encode(o Options, fields []Field, msg Message) ([]byte, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	w := &writer{o: o}
	if err := w.fields(fields, msg); err != nil {
		return nil, err
	}
	return w.b, nil
}

func (w *writer) fields(fields []Field, msg Message) error {
	for _, f := range fields {
		v := msg[f.Name]
		switch f.Type {
		case "string":
			s, _ := v.(string)
			if uint64(len(s)) >= 1<<(8*uint(w.o.StrLenBytes)) {
				return fmt.Errorf("field %s: string too long", f.Name)
			}
			w.uint(w.o.StrLenBytes, uint64(len(s)))
			w.b = append(w.b, s...)
			if w.o.StrNullTerm {
				w.b = append(w.b, 0)
			}
		case "array":
			var items []Message
			switch a := v.(type) {
			case []Message:
				items = a
			case nil:
			default:
				return fmt.Errorf("field %s: want []Message, got %T", f.Name, v)
			}
			w.uint(w.o.ArrLenBytes, uint64(len(items)))
			for _, it := range items {
				if err := w.fields(f.Fields, it); err != nil {
					return err
				}
			}
		default:
			n := intSize(f.Type)
			if n == 0 {
				return fmt.Errorf("field %s: unknown type %q", f.Name, f.Type)
			}
			var x int64
			if v != nil {
				var err error
				if x, err = toInt(v); err != nil {
					return fmt.Errorf("field %s: %w", f.Name, err)
				}
			}
			w.uint(n, uint64(x))
		}
	}
	return nil
}

type reader struct {
	b []byte
	o Options
}

func (r *reader) take(n int) ([]byte, error) {
	if n < 0 || len(r.b) < n {
		return nil, fmt.Errorf("%w: need %d bytes, have %d", ErrDecode, n, len(r.b))
	}
	p := r.b[:n]
	r.b = r.b[n:]
	return p, nil
}

func (r *reader) uint(n int) (uint64, error) {
	p, err := r.take(n)
	if err != nil {
		return 0, err
	}
	switch n {
	case 1:
		return uint64(p[0]), nil
	case 2:
		return uint64(r.o.ByteOrder.Uint16(p)), nil
	case 4:
		return uint64(r.o.ByteOrder.Uint32(p)), nil
	}
	return r.o.ByteOrder.Uint64(p), nil
}

// Decode parses payload according to fields. Trailing bytes are an error.
func Decode(o Options, fields []Field, payload []byte) (Message, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	r := &reader{b: payload, o: o}
	m, err := r.fields(fields)
	if err != nil {
		return nil, err
	}
	if len(r.b) != 0 {
		return nil, fmt.Errorf("%w: %d trailing bytes", ErrDecode, len(r.b))
	}
	return m, nil
}

func (r *reader) fields(fields []Field) (Message, error) {
	m := make(Message, len(fields))
	for _, f := range fields {
		switch f.Type {
		case "string":
			n, err := r.uint(r.o.StrLenBytes)
			if err != nil {
				return nil, err
			}
			p, err := r.take(int(n))
			if err != nil {
				return nil, err
			}
			m[f.Name] = string(p)
			if r.o.StrNullTerm {
				if _, err := r.take(1); err != nil {
					return nil, err
				}
			}
		case "array":
			n, err := r.uint(r.o.ArrLenBytes)
			if err != nil {
				return nil, err
			}
			if int(n) > len(r.b) { // every element is at least 1 byte unless it has no fields
				if len(f.Fields) > 0 {
					return nil, fmt.Errorf("%w: array %s count %d exceeds payload", ErrDecode, f.Name, n)
				}
			}
			items := make([]Message, 0, min(int(n), 1024))
			for i := 0; i < int(n); i++ {
				it, err := r.fields(f.Fields)
				if err != nil {
					return nil, err
				}
				items = append(items, it)
			}
			m[f.Name] = items
		default:
			sz := intSize(f.Type)
			if sz == 0 {
				return nil, fmt.Errorf("field %s: unknown type %q", f.Name, f.Type)
			}
			u, err := r.uint(sz)
			if err != nil {
				return nil, err
			}
			switch f.Type {
			case "int8":
				m[f.Name] = int64(int8(u))
			case "int16":
				m[f.Name] = int64(int16(u))
			case "int32":
				m[f.Name] = int64(int32(u))
			default:
				m[f.Name] = int64(u)
			}
		}
	}
	return m, nil
}
