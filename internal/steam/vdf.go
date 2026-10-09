package steam

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Binary VDF types (Valve KeyValues binary format).
const (
	vdfNone   = 0x00 // nested dict follows
	vdfString = 0x01
	vdfInt32  = 0x02
	vdfFloat  = 0x03
	vdfPtr    = 0x04
	vdfWide   = 0x05
	vdfColor  = 0x06
	vdfUInt64 = 0x07
	vdfEnd    = 0x08
)

// vdfValue is one parsed value.
type vdfValue struct {
	str   string
	i32   int32
	u64   uint64
	isStr bool
	isI32 bool
	isU64 bool
	dict  *vdfDict
}

// vdfDict preserves insertion order (Steam rewrites sequentially anyway).
type vdfDict struct {
	keys []string
	vals map[string]*vdfValue
}

func newDict() *vdfDict { return &vdfDict{vals: map[string]*vdfValue{}} }

func (d *vdfDict) set(k string, v *vdfValue) {
	if _, ok := d.vals[k]; !ok {
		d.keys = append(d.keys, k)
	}
	d.vals[k] = v
}

func (d *vdfDict) get(k string) *vdfValue { return d.vals[k] }

type vdfReader struct {
	data []byte
	pos  int
}

func (r *vdfReader) byte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *vdfReader) cstring() (string, error) {
	start := r.pos
	for {
		if r.pos >= len(r.data) {
			return "", io.ErrUnexpectedEOF
		}
		if r.data[r.pos] == 0x00 {
			s := string(r.data[start:r.pos])
			r.pos++
			return s, nil
		}
		r.pos++
	}
}

func (r *vdfReader) u32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.LittleEndian.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	return v, nil
}

func (r *vdfReader) u64() (uint64, error) {
	if r.pos+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.LittleEndian.Uint64(r.data[r.pos : r.pos+8])
	r.pos += 8
	return v, nil
}

// parseDict parses entries until 0x08.
func (r *vdfReader) parseDict() (*vdfDict, error) {
	d := newDict()
	for {
		t, err := r.byte()
		if err != nil {
			return nil, err
		}
		if t == vdfEnd {
			return d, nil
		}
		key, err := r.cstring()
		if err != nil {
			return nil, fmt.Errorf("vdf key: %w", err)
		}
		switch t {
		case vdfNone:
			sub, err := r.parseDict()
			if err != nil {
				return nil, fmt.Errorf("vdf dict %q: %w", key, err)
			}
			d.set(key, &vdfValue{dict: sub})
		case vdfString:
			s, err := r.cstring()
			if err != nil {
				return nil, fmt.Errorf("vdf string %q: %w", key, err)
			}
			d.set(key, &vdfValue{str: s, isStr: true})
		case vdfInt32:
			v, err := r.u32()
			if err != nil {
				return nil, fmt.Errorf("vdf int %q: %w", key, err)
			}
			d.set(key, &vdfValue{i32: int32(v), isI32: true})
		case vdfUInt64:
			v, err := r.u64()
			if err != nil {
				return nil, fmt.Errorf("vdf u64 %q: %w", key, err)
			}
			d.set(key, &vdfValue{u64: v, isU64: true})
		case vdfFloat, vdfPtr:
			v, err := r.u32()
			if err != nil {
				return nil, err
			}
			d.set(key, &vdfValue{i32: int32(v), isI32: true})
		case vdfWide:
			// Wide string: approximate as raw bytes until double null.
			// Rare in shortcuts.vdf; keep position sane.
			s, err := r.cstring()
			if err != nil {
				return nil, err
			}
			d.set(key, &vdfValue{str: s, isStr: true})
		case vdfColor:
			if r.pos+4 > len(r.data) {
				return nil, io.ErrUnexpectedEOF
			}
			r.pos += 4
			d.set(key, &vdfValue{isStr: true})
		default:
			return nil, fmt.Errorf("vdf: unknown type 0x%02x for key %q", t, key)
		}
	}
}

// decodeVDF decodes a full shortcuts.vdf document.
func decodeVDF(data []byte) (*vdfDict, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return newDict(), nil
	}
	r := &vdfReader{data: data}
	root := newDict()
	for r.pos < len(r.data) {
		t, err := r.byte()
		if err != nil {
			return nil, err
		}
		if t == vdfEnd {
			break
		}
		key, err := r.cstring()
		if err != nil {
			return nil, err
		}
		switch t {
		case vdfNone:
			sub, err := r.parseDict()
			if err != nil {
				return nil, err
			}
			root.set(key, &vdfValue{dict: sub})
		case vdfString:
			s, err := r.cstring()
			if err != nil {
				return nil, err
			}
			root.set(key, &vdfValue{str: s, isStr: true})
		case vdfInt32:
			v, err := r.u32()
			if err != nil {
				return nil, err
			}
			root.set(key, &vdfValue{i32: int32(v), isI32: true})
		default:
			return nil, fmt.Errorf("vdf: unexpected top-level type 0x%02x", t)
		}
	}
	return root, nil
}

// encodeVDF writes root dict.
func encodeVDF(root *vdfDict) []byte {
	var buf bytes.Buffer
	for _, k := range root.keys {
		v := root.vals[k]
		if v.dict != nil {
			buf.WriteByte(vdfNone)
			buf.WriteString(k)
			buf.WriteByte(0x00)
			encodeDict(&buf, v.dict)
		} else if v.isStr {
			buf.WriteByte(vdfString)
			buf.WriteString(k)
			buf.WriteByte(0x00)
			buf.WriteString(v.str)
			buf.WriteByte(0x00)
		} else if v.isI32 {
			buf.WriteByte(vdfInt32)
			buf.WriteString(k)
			buf.WriteByte(0x00)
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], uint32(v.i32))
			buf.Write(b[:])
		} else if v.isU64 {
			buf.WriteByte(vdfUInt64)
			buf.WriteString(k)
			buf.WriteByte(0x00)
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], v.u64)
			buf.Write(b[:])
		}
	}
	buf.WriteByte(vdfEnd)
	return buf.Bytes()
}

func encodeDict(buf *bytes.Buffer, d *vdfDict) {
	for _, k := range d.keys {
		v := d.vals[k]
		if v.dict != nil {
			buf.WriteByte(vdfNone)
			buf.WriteString(k)
			buf.WriteByte(0x00)
			encodeDict(buf, v.dict)
		} else if v.isStr {
			buf.WriteByte(vdfString)
			buf.WriteString(k)
			buf.WriteByte(0x00)
			buf.WriteString(v.str)
			buf.WriteByte(0x00)
		} else if v.isI32 {
			buf.WriteByte(vdfInt32)
			buf.WriteString(k)
			buf.WriteByte(0x00)
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], uint32(v.i32))
			buf.Write(b[:])
		} else if v.isU64 {
			buf.WriteByte(vdfUInt64)
			buf.WriteString(k)
			buf.WriteByte(0x00)
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], v.u64)
			buf.Write(b[:])
		}
	}
	buf.WriteByte(vdfEnd)
}
