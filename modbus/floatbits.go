package modbus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

type FloatBitsFormat string

var FloatFormats = map[FloatBitsFormat]struct{}{
	BCD:               {},
	FloatBigEndian:    {},
	FloatLittleEndian: {},
	IntBigEndian:      {},
	IntLittleEndian:   {},
}

const (
	BCD               FloatBitsFormat = "bcd"
	FloatBigEndian    FloatBitsFormat = "float_big_endian"
	FloatLittleEndian FloatBitsFormat = "float_little_endian"
	IntBigEndian      FloatBitsFormat = "int_big_endian"
	IntLittleEndian   FloatBitsFormat = "int_little_endian"
)

func (ff FloatBitsFormat) Validate() error {
	if _, f := FloatFormats[ff]; !f {
		return fmt.Errorf(`занчение строки формата должно быть из списка %s`, formatParamFormats())
	}
	return nil
}

func (ff FloatBitsFormat) PutFloat(d []byte, v float64) {
	switch ff {
	case BCD:
		PutBCD6(d, v)
	case FloatBigEndian:
		n := math.Float32bits(float32(v))
		binary.BigEndian.PutUint32(d, n)
	case FloatLittleEndian:
		n := math.Float32bits(float32(v))
		binary.LittleEndian.PutUint32(d, n)
	case IntBigEndian:
		n := int32(float32(v))
		binary.BigEndian.PutUint32(d, uint32(n))
	case IntLittleEndian:
		n := int32(float32(v))
		binary.LittleEndian.PutUint32(d, uint32(n))
	default:
		panic(ff)
	}
}

func (ff FloatBitsFormat) ParseFloat(d []byte) (float64, error) {
	d = d[:4]
	_ = d[0]
	_ = d[1]
	_ = d[2]
	_ = d[3]

	floatBits := func(endian binary.ByteOrder) (float64, error) {
		bits := endian.Uint32(d)
		x := float64(math.Float32frombits(bits))
		if math.IsNaN(x) {
			return x, fmt.Errorf("not a float %v number", endian)
		}
		return x, nil
	}
	intBits := func(endian binary.ByteOrder) float64 {
		bits := endian.Uint32(d)
		return float64(int32(bits))
	}

	var (
		be = binary.BigEndian
		le = binary.LittleEndian
	)

	switch ff {
	case BCD:
		if x, ok := ParseBCD6(d); ok {
			return x, nil
		} else {
			return 0, errors.New("not a BCD number")
		}
	case FloatBigEndian:
		return floatBits(be)
	case FloatLittleEndian:
		return floatBits(le)
	case IntBigEndian:
		return intBits(be), nil
	case IntLittleEndian:
		return intBits(le), nil
	default:
		return 0, fmt.Errorf("wrong float format %q", ff)
	}
}

func formatParamFormats() string {
	var xs []string
	for s := range FloatFormats {
		xs = append(xs, string(s))
	}
	return strings.Join(xs, ",")
}
