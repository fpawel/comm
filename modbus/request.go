package modbus

import (
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
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

type ResponseReader interface {
	GetResponse(*structlog.Logger, []byte, comm.ResponseParser) ([]byte, error)
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

func (x Request) GetResponse(logger *structlog.Logger, responseReader ResponseReader, parseResponse comm.ResponseParser) ([]byte, error) {
	return responseReader.GetResponse(logger, x.Bytes(), func(request, response []byte) (string, error) {
		if err := x.checkResponse(response); err != nil {
			return "", err
		}
		if parseResponse != nil {
			return parseResponse(request, response)
		}
		return "", nil
	})
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

func NewSwitchGasOvenRequest(n byte) Request {
	return Request{
		Addr:     0x16,
		ProtoCmd: 0x10,
		Data:     []byte{0, 32, 0, 1, 2, 0, n},
	}
}

func (x *Request) ParseBCDValue(b []byte) (v float64, err error) {
	if err = x.checkResponse(b); err != nil {
		return
	}
	if len(b) != 9 {
		err = ErrProtocol.Here().WithMessagef("ожидалось 9 байт ответа, %d байт получено", len(b))
		return
	}
	var ok bool
	if v, ok = ParseBCD6(b[3:]); !ok {
		err = ErrProtocol.Here().WithMessagef("не правильный код BCD: [% X]", b[3:7])
	}
	return
}

func (x Request) checkResponse(response []byte) error {

	if len(response) == 0 {
		return ErrProtocol.Here().WithMessage("нет ответа")
	}

	if len(response) < 4 {
		return ErrProtocol.Here().WithMessage("длина ответа меньше 4")
	}

	if h, l := CRC16(response); h != 0 || l != 0 {
		return ErrProtocol.Here().WithMessage("CRC16 не ноль")
	}
	if response[0] != byte(x.Addr) {
		return ErrProtocol.Here().WithMessage("несовпадение адресов запроса %d и ответа %d")
	}

	if len(response) == 5 && byte(x.ProtoCmd)|0x80 == response[1] {
		return ErrProtocol.Here().WithMessagef("аппаратная ошибка %d", response[2])
	}
	if response[1] != byte(x.ProtoCmd) {
		return ErrProtocol.Here().WithMessage("несовпадение кодов команд запроса и ответа")
	}

	return nil
}

var ErrProtocol = merry.WithMessage(comm.ErrProtocol, "MODBUS failed")