package modbus

import (
	"encoding/binary"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/hashicorp/go-multierror"
	"github.com/powerman/structlog"
	"strconv"
)

func Read3(logger *structlog.Logger, responseReader ResponseReader, addr Addr, firstReg Var, regsCount uint16, parseResponse comm.ResponseParser) ([]byte, error) {

	logger = withKeyValue(logger, "действие", fmt.Sprintf("считывание %d регистров начиная с %d", regsCount, firstReg))

	req := Req{
		Addr:     addr,
		ProtoCmd: 3,
		Data:     append(uint16b(uint16(firstReg)), uint16b(regsCount)...),
	}

	return req.GetResponse(logger, responseReader, func(request, response []byte) (string, error) {
		lenMustBe := int(regsCount)*2 + 5
		if len(response) != lenMustBe {
			return "", merry.Errorf("длина ответа %d не равна %d", len(response), lenMustBe)
		}
		if parseResponse != nil {
			return parseResponse(request, response)
		}
		return "", nil
	})
}

func Read3BCDValues(logger *structlog.Logger, responseReader ResponseReader, addr Addr, var3 Var, count int) ([]float64, error) {

	logger = withKeyValue(logger, "формат", fmt.Sprintf("%d значений BCD", count))

	var values []float64
	_, err := Read3(logger, responseReader, addr, var3, uint16(count*2),
		func(request, response []byte) (result string, err error) {
			for i := 0; i < count; i++ {
				n := 3 + i*4
				if v, ok := ParseBCD6(response[n:]); !ok {
					err = multierror.Append(err,
						fmt.Errorf("не правильный код BCD: позиция=%d BCD=%X", n, response[n:n+4]))
				} else {
					values = append(values, v)
					if len(result) > 0 {
						result += ", "
					}
					result += fmt.Sprintf("%v", v)
				}
			}
			return
		})
	return values, err

}

func ReadUInt16(logger *structlog.Logger, responseReader ResponseReader, addr Addr, var3 Var) (result uint16, err error) {
	logger = withKeyValue(logger, "формат", "uint16")
	_, err = Read3(logger, responseReader, addr, var3, 1,
		func(_, response []byte) (string, error) {
			result = binary.LittleEndian.Uint16(response[3:5])
			return strconv.Itoa(int(result)), nil
		})
	return
}

func Read3BCD(logger *structlog.Logger, responseReader ResponseReader, addr Addr, var3 Var) (result float64, err error) {
	logger = withKeyValue(logger, "формат", "bcd")
	_, err = Read3(logger, responseReader, addr, var3, 2,
		func(request []byte, response []byte) (string, error) {
			var ok bool
			if result, ok = ParseBCD6(response[3:]); !ok {
				return "", merry.Errorf("не правильный код BCD: % X", response[3:7])
			}
			return fmt.Sprintf("%v", result), nil
		})
	return
}

func Write32FloatProto(logger *structlog.Logger, responseReader ResponseReader, addr Addr, protocolCommandCode ProtoCmd, deviceCommandCode DevCmd, value float64) error {

	logger = withKeyValue(logger, "действие", fmt.Sprintf("отправка команды %d(%v)", deviceCommandCode, value))

	req := Write32BCDRequest(addr, protocolCommandCode, deviceCommandCode, value)

	_, err := req.GetResponse(logger, responseReader, func(request, response []byte) (string, error) {
		for i := 2; i < 6; i++ {
			if request[i] != response[i] {
				return "", ErrProtocol.Here().
					WithMessagef("ошибка формата: запрос[2:6]==[% X] != ответ[2:6]==[% X]", request[2:6], response[2:6])
			}
		}
		return "OK", nil
	})
	return err
}

func withKeyValue(logger *structlog.Logger, key string, value interface{}) *structlog.Logger {
	return logger.New(key, value).PrependSuffixKeys(key)
}
