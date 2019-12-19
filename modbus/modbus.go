package modbus

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/internal"
	"github.com/powerman/structlog"
	"io"
	"strconv"
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

type responseParserFunc = func(request, response []byte) (string, error)

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
	cfg comm.Config,
	rw io.ReadWriter, addr Addr,
	firstReg Var, regsCount uint16,
	parseResponse comm.ParseResponseFunc) ([]byte, error) {

	log = internal.LogPrependSuffixKeys(log,
		LogKeyRegsCount, regsCount,
		LogKeyFirstReg, firstReg,
	)

	req := RequestRead3(addr, firstReg, regsCount)

	b, err := req.GetResponse(log, ctx, cfg, rw, func(request, response []byte) (string, error) {
		lenMustBe := int(regsCount)*2 + 5
		if len(response) != lenMustBe {
			return "", merry.Errorf("длина ответа %d не равна %d", len(response), lenMustBe)
		}
		if parseResponse != nil {
			return parseResponse(request, response)
		}
		return "", nil
	})
	return b, merry.Appendf(err, "регистр %d: %d регистров", firstReg, regsCount)
}

func Read3BCDs(log comm.Logger, ctx context.Context, cfg comm.Config, rw io.ReadWriter, addr Addr, var3 Var, count int) ([]float64, error) {
	//log = logPrependSuffixKeys(log, "format", "BCD", "values_count", count)
	var values []float64
	_, err := Read3(log, ctx, cfg, rw, addr, var3, uint16(count*2),
		func(request, response []byte) (string, error) {
			var result string
			for i := 0; i < count; i++ {
				n := 3 + i*4
				v, ok := ParseBCD6(response[n:])
				if !ok {
					return "", comm.Err.Here().Appendf("не правильный код BCD % X, позиция %d", response[n:n+4], n)
				}
				values = append(values, v)
				if len(result) > 0 {
					result += ", "
				}
				result += fmt.Sprintf("%v", v)
			}
			return result, nil
		})
	return values, err

}

func Read3UInt16(log comm.Logger, ctx context.Context, cfg comm.Config, rw io.ReadWriter, addr Addr, var3 Var, byteOrder binary.ByteOrder) (uint16, error) {
	//log = logPrependSuffixKeys(log, "format", "uint16")
	var result uint16
	_, err := Read3(log, ctx, cfg, rw, addr, var3, 1,
		func(_, response []byte) (string, error) {
			result = byteOrder.Uint16(response[3:5])
			return strconv.Itoa(int(result)), nil
		})
	return result, merry.Append(err, "запрос числа в uin16")
}

func Read3BCD(log comm.Logger, ctx context.Context, cfg comm.Config, rw io.ReadWriter, addr Addr, var3 Var) (float64, error) {
	//log = logPrependSuffixKeys(log, "format", "bcd")
	var result float64
	_, err := Read3(log, ctx, cfg, rw, addr, var3, 2,
		func(request []byte, response []byte) (string, error) {
			var ok bool
			if result, ok = ParseBCD6(response[3:]); !ok {
				return "", comm.Err.Here().Appendf("не правильный код BCD % X", response[3:7])
			}
			return fmt.Sprintf("%v", result), nil
		})
	return result, merry.Append(err, "запрос числа в BCD")
}

func Write32(log comm.Logger, ctx context.Context, cfg comm.Config, rw io.ReadWriter, addr Addr, protocolCommandCode ProtoCmd,
	deviceCommandCode DevCmd, value float64) error {
	log = internal.LogPrependSuffixKeys(log,
		LogKeyDeviceCmd, deviceCommandCode,
		LogKeyDeviceCmdArg, value,
	)
	req := NewWrite32BCDRequest(addr, protocolCommandCode, deviceCommandCode, value)
	_, err := req.GetResponse(log, ctx, cfg, rw, func(request, response []byte) (string, error) {
		for i := 2; i < 6; i++ {
			if request[i] != response[i] {
				return "", merry.Appendf(Err.Here(),
					"ошибка формата: запрос[2:6]==[% X] != ответ[2:6]==[% X]", request[2:6], response[2:6])
			}
		}
		return "OK", nil
	})
	return merry.Appendf(err, "запись регистр=32 команда=%d аргумент=%v",
		deviceCommandCode, value)
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

func (x Request) GetResponse(log comm.Logger, ctx context.Context, cfg comm.Config, rw io.ReadWriter, prs comm.ParseResponseFunc) ([]byte, error) {
	log = internal.LogPrependSuffixKeys(log,
		LogKeyAddr, x.Addr,
		LogKeyCmd, x.ProtoCmd,
		LogKeyData, x.Data)
	b, err := comm.GetResponse(log, ctx, cfg, rw, x.Bytes(), func(request, response []byte) (s string, err error) {
		if err := x.checkResponse(response); err != nil {
			return "", err
		}
		if prs != nil {
			return prs(request, response)
		}
		return "", nil
	})
	return b, merry.Appendf(err, "modbus адрес=%d команда=%d", x.Addr, x.ProtoCmd)
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
