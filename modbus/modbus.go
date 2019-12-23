package modbus

import (
	"context"
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
