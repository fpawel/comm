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

var (
	Err            = merry.New("не верный ответ модбас").WithCause(comm.Err)
	ErrCRC16       = merry.New("несовпадение CRC16 в ответе модбас").WithCause(Err)
	ErrFloatFormat = merry.New("не верный формат числа с плавающей точкой в ответе модбас")
)

const (
	LogKeyAddr         = "модбас_адресс"
	LogKeyCmd          = "модбас_команда"
	LogKeyData         = "модбас_данные"
	LogKeyRegsCount    = "модбас_число_регистров"
	LogKeyFirstReg     = "модбас_регистр"
	LogKeyDeviceCmd    = "модбас_запись32"
	LogKeyDeviceCmdArg = "модбас_аргумент"
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
	return b, merry.Appendf(err, "модбас[адрес %d команда %d]", x.Addr, x.ProtoCmd)
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
	if v, err = ParseBCD6(b[3:]); !ok {
		n := 3
		err = Err.Here().WithCause(merry.Appendf(err, "поз.%d подстрока % X, ожидалось число BCD", n, b[n:n+4]))
	}
	return
}

func (x Request) checkResponse(response []byte) error {

	if len(response) == 0 {
		return Err.Here().Append("нет ответа модбас")
	}

	if len(response) < 4 {
		return Err.Here().Append("длина ответа модбас меньше 4")
	}

	if h, l := CRC16(response); h != 0 || l != 0 {
		return ErrCRC16.Here()
	}
	if response[0] != byte(x.Addr) {
		return Err.Here().Append("несовпадение адресов модбас запроса %d и ответа %d")
	}

	if len(response) == 5 && byte(x.ProtoCmd)|0x80 == response[1] {
		return Err.Here().Appendf("код ошибки модбас %d", response[2])
	}
	if response[1] != byte(x.ProtoCmd) {
		return Err.Here().Append("несовпадение кодов команд модбас запроса и ответа")
	}

	return nil
}
