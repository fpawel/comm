package modbus

import (
	"context"
	"encoding/binary"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/internal"
)

type RequestRead3 struct {
	Addr           Addr
	FirstRegister  Var
	RegistersCount uint16
}

func (x RequestRead3) Request() Request {
	return Request{
		Addr:     x.Addr,
		ProtoCmd: 3,
		Data: []byte{
			byte(x.FirstRegister >> 8),
			byte(x.FirstRegister),
			byte(x.RegistersCount >> 8),
			byte(x.RegistersCount),
		},
	}
}

func (x RequestRead3) GetResponse(log comm.Logger, ctx context.Context, cm comm.T) ([]byte, error) {
	log = internal.LogPrependSuffixKeys(log,
		LogKeyRegsCount, x.RegistersCount,
		LogKeyFirstReg, x.FirstRegister,
	)
	cm = cm.WithAppendParse(func(request, response []byte) error {
		lenMustBe := int(x.RegistersCount)*2 + 5
		if len(response) != lenMustBe {
			return merry.Errorf("длина ответа %d, а должна быть %d", len(response), lenMustBe)
		}
		return nil
	})
	b, err := x.Request().GetResponse(log, ctx, cm)
	return b, merry.Appendf(err, "регистр %d: %d регистров", x.FirstRegister, x.RegistersCount)
}

func Read3Values(log comm.Logger, ctx context.Context, cm comm.T, addr Addr, var3 Var, count int, format FloatBitsFormat) ([]float64, error) {
	var values []float64
	response, err := RequestRead3{
		Addr:           addr,
		FirstRegister:  var3,
		RegistersCount: uint16(count * 2),
	}.GetResponse(log, ctx, cm)
	if err != nil {
		return nil, merry.Appendf(err, "запрос %d значений %s", count, format)
	}
	for i := 0; i < count; i++ {
		n := 3 + i*4
		v, err := format.ParseFloat(response[n:][:4])
		if err != nil {
			return nil, merry.Appendf(err, "not a number %s: % X: позиция %d", format, response[n:n+4], n)
		}
		values = append(values, v)
	}
	return values, err
}

func Read3Value(log comm.Logger, ctx context.Context, cm comm.T, addr Addr, var3 Var, format FloatBitsFormat) (float64, error) {
	response, err := RequestRead3{
		Addr:           addr,
		FirstRegister:  var3,
		RegistersCount: 2,
	}.GetResponse(log, ctx, cm)
	if err != nil {
		return 0, merry.Appendf(err, "запрос значения %s", format)
	}
	result, ok := ParseBCD6(response[3:7])
	if !ok {
		return 0, merry.Appendf(err, "not a number %s: % X", format, response[3:7])
	}
	return result, nil
}

func Read3UInt16(log comm.Logger, ctx context.Context, cm comm.T, addr Addr, var3 Var, byteOrder binary.ByteOrder) (uint16, error) {
	response, err := RequestRead3{
		Addr:           addr,
		FirstRegister:  var3,
		RegistersCount: 1,
	}.GetResponse(log, ctx, cm)
	if err != nil {
		return 0, merry.Append(err, "запрос числа uin16")
	}
	return byteOrder.Uint16(response[3:5]), nil
}
