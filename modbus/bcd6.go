package modbus

import (
	"github.com/ansel1/merry"
	"math"
)

func ParseBCD6(b []byte) (float64, error) {
	_ = b[3] // early bounds check to guarantee safety of writes below
	var (
		x, y float64
		ok   bool
		r    float64
	)
	if x, y, ok = dec2(b[1]); !ok {
		return 0, merry.Errorf("недопустимое значение первого байта %X", b[1])
	}
	r += 100000*x + 10000*y

	if x, y, ok = dec2(b[2]); !ok {
		return 0, merry.Errorf("недопустимое значение второго байта %X", b[2])
	}
	r += 1000*x + 100*y

	if x, y, ok = dec2(b[3]); !ok {
		return 0, merry.Errorf("недопустимое значение третьего байта %X", b[3])
	}
	r += 10*x + y

	coma := float64(b[0] & 0x7)
	sign := float64(-1)
	if b[0]>>7 == 0 {
		sign = 1
	}
	r *= sign
	r /= math.Pow(10, coma)

	return r, nil
}

func dec2(b byte) (v1 float64, v2 float64, ok bool) {
	v1, v2 = float64(b>>4), float64(b&0xF)
	ok = v1 > -1 && v2 > -1 && v1 < 10 && v2 < 10
	return
}

func BCD6(x float64) []byte {
	b := make([]byte, 4)
	PutBCD6(b, x)
	return b
}

func PutBCD6(b []byte, x float64) {

	b[0] = 0
	if x < 0 {
		b[0] |= 0x80
	}
	x = math.Abs(x)
	for i := byte(0); i < 6; i++ {
		if x >= bcdComa[i] && x < bcdComa[i+1] {
			b[0] |= 6 - i
			break
		}
	}
	if x < 1 {
		x *= 1000000
	} else {
		for x < 100000 {
			x *= 10
		}
	}
	v := int64(math.Round(x))
	b[1] = byte(v/100000) << 4
	v %= 100000
	b[1] += byte(v / 10000)
	v %= 10000
	b[2] = byte(v/1000) << 4
	v %= 1000
	b[2] += byte(v / 100)
	v %= 100
	b[3] = byte(v/10) << 4
	v %= 10
	b[3] += byte(v)
	return
}

var bcdComa = []float64{0, 1, 10, 100, 1000, 10000, 100000}
