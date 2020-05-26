package modbus

import (
	"context"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/internal"
)

type RequestWrite32 struct {
	Addr      Addr
	ProtoCmd  ProtoCmd
	DeviceCmd DevCmd
	Format    FloatBitsFormat
	Value     float64
}

func (x RequestWrite32) Request() Request {
	r := Request{
		Addr:     x.Addr,
		ProtoCmd: x.ProtoCmd,
	}
	r.Data = []byte{
		0, 32, 0, 3, 6,
		byte(x.DeviceCmd >> 8),
		byte(x.DeviceCmd),
		0, 0, 0, 0,
	}
	x.Format.PutFloat(r.Data[7:], x.Value)
	return r
}

func (x RequestWrite32) GetResponse(log comm.Logger, ctx context.Context, cm comm.T) error {
	log = internal.LogPrependSuffixKeys(log,
		LogKeyDeviceCmd, x.DeviceCmd,
		LogKeyDeviceCmdArg, x.Value,
		"float_bits_format", x.Format,
	)

	wrapErr := func(err error) error {
		return merry.Appendf(err, "запись в прибор (команда=%d аргумент=%v %s)",
			x.DeviceCmd, x.Value, x.Format)
	}

	req := x.Request()
	response, err := req.GetResponse(log, ctx, cm)
	if err != nil {
		return wrapErr(err)
	}
	request := req.Bytes()
	for i := 2; i < 6; i++ {
		if request[i] != response[i] {
			err = merry.Appendf(Err.Here(),
				"ошибка формата: запрос[2:6]==[% X] != ответ[2:6]==[% X]", request[2:6], response[2:6])
			return wrapErr(err)
		}
	}
	return nil
}
