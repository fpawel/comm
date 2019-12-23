package modbus

import (
	"context"
	"encoding/binary"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/internal"
	"github.com/powerman/structlog"
)

type ProtoCmd byte
type Addr byte

type Var uint16

type Request struct {
	Addr     Addr
	ProtoCmd ProtoCmd
	Data     []byte
}

type DevCmd uint16

type Coefficient uint16

var Err = merry.Append(comm.Err, "ошибка проткола modbus")

const (
	LogKeyAddr         = "modbus_address"
	LogKeyCmd          = "modbus_cmd"
	LogKeyData         = "modbus_data"
	LogKeyRegsCount    = "modbus_regs_count"
	LogKeyFirstReg     = "modbus_first_register"
	LogKeyDeviceCmd    = "modbus_device_cmd"
	LogKeyDeviceCmdArg = "modbus_device_cmd_arg"
)

func SetLogKeysFormat() {
	structlog.DefaultLogger.SetKeysFormat(
		map[string]string{
			LogKeyData: " %[1]s=`% [2]X`",
		})
}

func RequestRead3(addr Addr, firstRegister Var, registersCount uint16) Request {
	return Request{
		Addr:     addr,
		ProtoCmd: 3,
		Data:     append(uint16b(uint16(firstRegister)), uint16b(registersCount)...),
	}
}

func Read3(log comm.Logger,
	ctx context.Context,
	cm comm.T, addr Addr,
	firstReg Var, regsCount uint16) ([]byte, error) {

	log = internal.LogPrependSuffixKeys(log,
		LogKeyRegsCount, regsCount,
		LogKeyFirstReg, firstReg,
	)
	cm = cm.WithAppendParse(func(request, response []byte) error {
		lenMustBe := int(regsCount)*2 + 5
		if len(response) != lenMustBe {
			return merry.Errorf("длина ответа %d, а должна быть %d", len(response), lenMustBe)
		}
		return nil
	})
	b, err := RequestRead3(addr, firstReg, regsCount).GetResponse(log, ctx, cm)
	return b, merry.Appendf(err, "регистр %d: %d регистров", firstReg, regsCount)
}

func Read3BCDs(log comm.Logger, ctx context.Context, cm comm.T, addr Addr, var3 Var, count int) ([]float64, error) {
	var values []float64
	response, err := Read3(log, ctx, cm, addr, var3, uint16(count*2))
	for i := 0; i < count; i++ {
		n := 3 + i*4
		v, ok := ParseBCD6(response[n:])
		if !ok {
			return nil, comm.Err.Here().Appendf("не правильный код BCD % X, позиция %d", response[n:n+4], n)
		}
		values = append(values, v)
	}
	return values, err
}

func Read3UInt16(log comm.Logger, ctx context.Context, cm comm.T, addr Addr, var3 Var, byteOrder binary.ByteOrder) (uint16, error) {
	response, err := Read3(log, ctx, cm, addr, var3, 1)
	if err != nil {
		return 0, merry.Append(err, "запрос числа uin16")
	}
	return byteOrder.Uint16(response[3:5]), nil
}

func Read3BCD(log comm.Logger, ctx context.Context, cm comm.T, addr Addr, var3 Var) (float64, error) {
	response, err := Read3(log, ctx, cm, addr, var3, 2)
	if err != nil {
		return 0, merry.Append(err, "запрос числа BCD")
	}
	result, ok := ParseBCD6(response[3:7])
	if !ok {
		return 0, comm.Err.Here().Appendf("не правильный код BCD % X", response[3:7])
	}
	return result, nil
}

func Write32(log comm.Logger, ctx context.Context, cm comm.T, addr Addr, protocolCommandCode ProtoCmd,
	deviceCommandCode DevCmd, value float64) error {
	log = internal.LogPrependSuffixKeys(log,
		LogKeyDeviceCmd, deviceCommandCode,
		LogKeyDeviceCmdArg, value,
	)
	req := NewWrite32BCDRequest(addr, protocolCommandCode, deviceCommandCode, value)
	response, err := req.GetResponse(log, ctx, cm)
	if err != nil {
		return merry.Appendf(err, "запись регистр=32 команда=%d аргумент=%v", deviceCommandCode, value)
	}
	request := req.Bytes()
	for i := 2; i < 6; i++ {
		if request[i] != response[i] {
			return merry.Appendf(Err.Here(),
				"ошибка формата: запрос[2:6]==[% X] != ответ[2:6]==[% X]", request[2:6], response[2:6]).
				Appendf("запись регистр=32 команда=%d аргумент=%v", deviceCommandCode, value)
		}
	}
	return nil
}

func (x Request) Bytes() (b []byte) {
	b = make([]byte, 4+len(x.Data))
	b[0] = byte(x.Addr)
	b[1] = byte(x.ProtoCmd)
	copy(b[2:], x.Data)
	n := 2 + len(x.Data)
	b[n], b[n+1] = CRC16(b[:n])
	return
}

func (x Request) GetResponse(log comm.Logger, ctx context.Context, cm comm.T) ([]byte, error) {
	log = internal.LogPrependSuffixKeys(log,
		LogKeyAddr, x.Addr,
		LogKeyCmd, x.ProtoCmd,
		LogKeyData, x.Data)
	cm = cm.WithPrependParse(func(request, response []byte) error {
		if err := x.checkResponse(response); err != nil {
			return err
		}
		return nil
	})
	b, err := cm.GetResponse(log, ctx, x.Bytes())
	return b, merry.Appendf(err, "protocol_addr=%d protocol_command=%d", x.Addr, x.ProtoCmd)
}

func NewWrite32BCDRequest(addr Addr, protocolCommandCode ProtoCmd, deviceCommandCode DevCmd,
	value float64) Request {
	r := Request{
		Addr:     addr,
		ProtoCmd: protocolCommandCode,
	}
	r.Data = []byte{0, 32, 0, 3, 6}
	r.Data = append(r.Data, uint16b(uint16(deviceCommandCode))...)
	r.Data = append(r.Data, BCD6(value)...)
	return r
}

func (x *Request) ParseBCDValue(b []byte) (v float64, err error) {
	if err = x.checkResponse(b); err != nil {
		return
	}
	if len(b) != 9 {
		err = Err.Here().Appendf("ожидалось 9 байт ответа, %d байт получено", len(b))
		return
	}
	var ok bool
	if v, ok = ParseBCD6(b[3:]); !ok {
		err = Err.Here().Appendf("не правильный код BCD: [% X]", b[3:7])
	}
	return
}

func (x Request) checkResponse(response []byte) error {

	if len(response) == 0 {
		return Err.Here().Append("нет ответа")
	}

	if len(response) < 4 {
		return Err.Here().Append("длина ответа меньше 4")
	}

	if h, l := CRC16(response); h != 0 || l != 0 {
		return Err.Here().Append("CRC16 не ноль")
	}
	if response[0] != byte(x.Addr) {
		return Err.Here().Append("несовпадение адресов запроса %d и ответа %d")
	}

	if len(response) == 5 && byte(x.ProtoCmd)|0x80 == response[1] {
		return Err.Here().Appendf("код ошибки прибора %d", response[2]).WithUserMessagef("код ошибки прибора %d", response[2])
	}
	if response[1] != byte(x.ProtoCmd) {
		return Err.Here().Append("несовпадение кодов команд запроса и ответа")
	}

	return nil
}

func uint16b(v uint16) (b []byte) {
	b = make([]byte, 2)
	b[0] = byte(v >> 8)
	b[1] = byte(v)
	return
}
