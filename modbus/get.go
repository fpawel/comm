package modbus

import (
	"encoding/binary"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/hashicorp/go-multierror"
	"github.com/powerman/structlog"
)

func Read3(logger *structlog.Logger, responseReader ResponseReader, addr Addr, firstReg Var, regsCount uint16, parseResponse comm.ResponseParser) ([]byte, error) {
	req := Req{
		Addr:     addr,
		ProtoCmd: 3,
		Data:     append(uint16b(uint16(firstReg)), uint16b(regsCount)...),
	}

	response, err := req.GetResponse(logger, responseReader, func(request, response []byte) (string, error) {
		lenMustBe := int(regsCount)*2 + 5
		if len(response) != lenMustBe {
			return "", merry.Errorf("длина ответа %d не равна %d",  len(response), lenMustBe)
		}
		if parseResponse != nil {
			return parseResponse(request, response)
		}
		return "", nil
	})

	if err != nil {
		//err = merry.Appendf(err, "чтение регистр=%d количество_регистров=%d", firstReg, regsCount)
		return response, logger.Err(err,
			"MODBUS", "считывание",
			"регистр", firstReg,
			"количество_регистров", regsCount,
		)
	}

	return response, nil
}

func Read3BCDValues(responseReader ResponseReader, addr Addr, var3 Var, count int) ([]float64, error) {
	var values []float64
	what := fmt.Sprintf("адрес %d: регистр %d: запрос %d значений BCD", addr, var3, count)
	_, err := Read3(what, responseReader, addr, var3, uint16(count*2),
		func(request, response []byte) (result string, err error) {
			for i := 0; i < count; i++ {
				n := 3 + i*4
				if v, ok := ParseBCD6(response[n:]); !ok {
					err = multierror.Append(err,
						fmt.Errorf("не правильный код BCD: позиция=%d BCD=%X", n, response[n:n+4]))
				} else {
					values = append(values, v)
					if len(result)>0 {
						result += ", "
					}
					result += fmt.Sprintf("%v", v)
				}
			}
			return
		})
	if err != nil {
		err = merry.Appendf(err, "запрос %d значений BCD", count)
	}
	return values, err

}

func Read3BCD(responseReader ResponseReader, addr Addr, var3 Var) (result float64, err error) {
	what := fmt.Sprintf("адрес %d: регистр %d: запрос значения BCD", addr, var3)
	_, err = Read3(what, responseReader, addr, var3, 2,
		func(request []byte, response []byte) (string, error) {
			var ok bool
			if result, ok = ParseBCD6(response[3:]); !ok {
				return "", merry.Errorf("не правильный код BCD: % X", response[3:7])
			}
			return fmt.Sprintf("%v", result), nil
		})
	if err != nil {
		err = merry.Append(err, "запрос значения BCD")
	}
	return
}

func Write32FloatProto(responseReader ResponseReader, addr Addr, protocolCommandCode ProtoCmd,
	deviceCommandCode DevCmd, value float64) error {
	req := Write32BCDRequest(addr, protocolCommandCode, deviceCommandCode, value)

	_, err := req.GetResponse(responseReader, func(request, response []byte) error {
		for i := 2; i < 6; i++ {
			if request[i] != response[i] {
				return ErrProtocol.Here().
					WithMessagef("ошибка формата ответа: [% X] != [% X]", request[2:6], response[2:6])
			}
		}
		return nil
	})

	if err != nil {
		return structlog.New().Err(err,
			"действие", "запись",
			"регистр", 32,
			"команда", deviceCommandCode,
			"записываемое_значение", value,
		)
	}
	return nil
}

func ReadUInt16(responseReader ResponseReader, addr Addr, var3 Var) (result uint16, err error) {
	_, err = Read3(responseReader, addr, var3, 1,
		func(_, response []byte) error {
			result = binary.LittleEndian.Uint16(response[3:5])
			return nil
		})
	return
}