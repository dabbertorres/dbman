package dbman

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

type (
	nullString  struct{ sql.NullString }
	nullBool    struct{ sql.NullBool }
	nullInt64   struct{ sql.NullInt64 }
	nullInt32   struct{ sql.NullInt32 }
	nullFloat64 struct{ sql.NullFloat64 }
	nullTime    struct{ sql.NullTime }
)

func (s nullString) String() string {
	if s.Valid {
		return s.NullString.String
	}
	return "NULL"
}

func (b nullBool) String() string {
	if b.Valid {
		return strconv.FormatBool(b.Bool)
	}
	return "NULL"
}

func (i nullInt64) String() string {
	if i.Valid {
		return strconv.FormatInt(i.Int64, 10)
	}
	return "NULL"
}

func (i nullInt32) String() string {
	if i.Valid {
		return strconv.FormatInt(int64(i.Int32), 10)
	}
	return "NULL"
}

func (f nullFloat64) String() string {
	if f.Valid {
		return strconv.FormatFloat(f.Float64, 'g', -1, 64)
	}
	return "NULL"
}

func (t nullTime) String() string {
	if t.Valid {
		return t.Time.String()
	}
	return "NULL"
}

type nullInt16 struct {
	Int16 int16
	Valid bool
}

func (i *nullInt16) Scan(v interface{}) error {
	switch rv := v.(type) {
	case nil:
		i.Int16 = 0
		i.Valid = false
		return nil

	case int64:
		i.Int16 = int16(rv)
		i.Valid = true
		return nil

	default:
		return fmt.Errorf("unexpected type '%T'", v)
	}
}

func (i nullInt16) String() string {
	if i.Valid {
		return strconv.FormatInt(int64(i.Int16), 10)
	}
	return "NULL"
}

type nullFloat32 struct {
	Float32 float32
	Valid   bool
}

func (f *nullFloat32) Scan(v interface{}) error {
	switch rv := v.(type) {
	case nil:
		f.Float32 = 0
		f.Valid = false
		return nil

	case float64:
		f.Float32 = float32(rv)
		f.Valid = true
		return nil

	default:
		return fmt.Errorf("unexpected type '%T'", v)
	}
}

func (f nullFloat32) String() string {
	if f.Valid {
		return strconv.FormatFloat(float64(f.Float32), 'g', -1, 32)
	}
	return "NULL"
}

type nullValue struct{}

func (nullValue) String() string {
	return "NULL"
}

type uuidVal struct {
	UUID  [16]byte
	Valid bool
}

func uuidParse(dst, in []byte) error {
	switch len(in) {
	case 16: // raw
		copy(dst[:], in)
		return nil

	case 32: // textual
		_, err := hex.Decode(dst[:], in)
		return err

	case 36: // textual with hyphens
		_, err := hex.Decode(dst[0:4], in[0:8])
		if err != nil {
			return err
		}

		_, err = hex.Decode(dst[4:6], in[9:13])
		if err != nil {
			return err
		}

		_, err = hex.Decode(dst[6:8], in[14:18])
		if err != nil {
			return err
		}

		_, err = hex.Decode(dst[8:10], in[19:23])
		if err != nil {
			return err
		}

		_, err = hex.Decode(dst[10:16], in[24:36])
		if err != nil {
			return err
		}
		return nil

	default:
		return fmt.Errorf("unexpected number of bytes: %d", len(in))
	}
}

func (v *uuidVal) Scan(raw interface{}) error {
	switch rv := raw.(type) {
	case string:
		if err := uuidParse(v.UUID[:], []byte(rv)); err != nil {
			return err
		}
		v.Valid = true
		return nil

	case []byte:
		if err := uuidParse(v.UUID[:], rv); err != nil {
			return err
		}
		v.Valid = true
		return nil

	case nil:
		v.Valid = false
		return nil

	default:
		return fmt.Errorf("unexpected type: %T", raw)
	}
}

func (v uuidVal) String() string {
	if !v.Valid {
		return "NULL"
	}

	var sb strings.Builder
	enc := hex.NewEncoder(&sb)

	enc.Write(v.UUID[0:4])
	sb.WriteByte('-')
	enc.Write(v.UUID[4:6])
	sb.WriteByte('-')
	enc.Write(v.UUID[6:8])
	sb.WriteByte('-')
	enc.Write(v.UUID[8:10])
	sb.WriteByte('-')
	enc.Write(v.UUID[10:16])

	return sb.String()
}
