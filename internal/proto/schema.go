package proto

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
)

// protocols.json is exported from proto_mate.lua of the recovered client
// (outputs/readable-source/interfaces/protocols.json). It is the field table
// only; it says nothing about the original server's behaviour.
//
//go:embed protocols.json
var protocolsJSON []byte

type Field struct {
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Fields []Field `json:"fields,omitempty"`
}

// fieldList tolerates the exporter writing an empty Lua table as {} instead of [].
type fieldList []Field

func (l *fieldList) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '{' {
		*l = nil
		return nil
	}
	var v []Field
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*l = v
	return nil
}

func (f *Field) UnmarshalJSON(b []byte) error {
	var a struct {
		Name   string    `json:"name"`
		Type   string    `json:"type"`
		Fields fieldList `json:"fields"`
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*f = Field{Name: a.Name, Type: a.Type, Fields: a.Fields}
	return nil
}

type Schema struct {
	// Send: client -> server. Recv: server -> client (names from the client's view).
	Send map[uint16][]Field
	Recv map[uint16][]Field
}

var (
	schemaOnce sync.Once
	schema     *Schema
	schemaErr  error
)

// LoadSchema parses the embedded field tables once.
func LoadSchema() (*Schema, error) {
	schemaOnce.Do(func() {
		var raw struct {
			Send map[string]fieldList `json:"send"`
			Recv map[string]fieldList `json:"recv"`
		}
		if err := json.Unmarshal(protocolsJSON, &raw); err != nil {
			schemaErr = err
			return
		}
		conv := func(m map[string]fieldList) (map[uint16][]Field, error) {
			out := make(map[uint16][]Field, len(m))
			for k, v := range m {
				n, err := strconv.ParseUint(k, 10, 16)
				if err != nil {
					return nil, fmt.Errorf("bad command id %q: %w", k, err)
				}
				out[uint16(n)] = []Field(v)
			}
			return out, nil
		}
		s := &Schema{}
		if s.Send, schemaErr = conv(raw.Send); schemaErr != nil {
			return
		}
		if s.Recv, schemaErr = conv(raw.Recv); schemaErr != nil {
			return
		}
		schema = s
	})
	return schema, schemaErr
}
