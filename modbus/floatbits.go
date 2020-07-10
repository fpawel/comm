package modbus

import (
	"encoding/binary"
	"github.com/ansel1/merry"
	"math"
	"strconv"
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
		return merry.Errorf(`занчение строки формата должно быть из списка %s`, formatParamFormats())
	}
	return nil
}

func (ff FloatBitsFormat) PutFloat(d []byte, v float64) error {
	if len(d) < 4 {
		return merry.Errorf("FloatBitsFormat.PutFloat: output bytes out of range: 4 bytes awaits, got %d", len(d))
	}
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
		return merry.Errorf(`занчение строки формата должно быть из списка %s`, formatParamFormats())
	}
	return nil
}

func (ff FloatBitsFormat) ParseFloat(d []byte) (float64, error) {
	if len(d) < 4 {
		return 0, merry.Errorf("FloatBitsFormat.ParseFloat: input bytes out of range: 4 bytes awaits, got %d", len(d))
	}
	d = d[:4]

	floatBits := func(endian binary.ByteOrder) (float64, error) {
		bits := endian.Uint32(d)
		f32 := math.Float32frombits(bits)
		str := strconv.FormatFloat(float64(f32), 'f', -1, 32)
		f64, _ := strconv.ParseFloat(str, 64)

		if math.IsNaN(f64) {
			return f64, merry.New("NaN")
		}
		if math.IsInf(f64, -1) {
			return f64, merry.New("-Infinity")
		}
		if math.IsInf(f64, +1) {
			return f64, merry.New("+Infinity")
		}
		if math.IsInf(f64, 0) {
			return f64, merry.New("0Infinity")
		}
		return f64, nil
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
		return ParseBCD6(d)
	case FloatBigEndian:
		return floatBits(be)
	case FloatLittleEndian:
		return floatBits(le)
	case IntBigEndian:
		return intBits(be), nil
	case IntLittleEndian:
		return intBits(le), nil
	default:
		return 0, merry.Errorf("wrong float format %q", ff)
	}
}

func formatParamFormats() string {
	var xs []string
	for s := range FloatFormats {
		xs = append(xs, string(s))
	}
	return strings.Join(xs, ",")
}
