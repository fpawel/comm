package modbus

import (
	"encoding/binary"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/powerman/structlog"
	"strconv"
)

const keyModbus = "_modbus"

func Read3(logger *structlog.Logger, responseReader ResponseReader, addr Addr, firstReg Var, regsCount uint16, parseResponse comm.ResponseParser) ([]byte, error) {

	logger = comm.LogWithKeys(logger,
		keyModbus, "считывание",
		"количество_регистров", regsCount,
		"регистр", firstReg,
	)

	req := Request{
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

func Read3BCDs(logger *structlog.Logger, responseReader ResponseReader, addr Addr, var3 Var, count int) ([]float64, error) {

	logger = comm.LogWithKeys(logger, "формат", "BCD", "количество_значений", count)

	var values []float64
	_, err := Read3(logger, responseReader, addr, var3, uint16(count*2),
		func(request, response []byte) (string, error) {
			var result string
			for i := 0; i < count; i++ {
				n := 3 + i*4
				v, ok := ParseBCD6(response[n:])
				if !ok {
					return "", merry.Errorf("не правильный код BCD % X, позиция %d", response[n:n+4], n)
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

func Read3UInt16(logger *structlog.Logger, responseReader ResponseReader, addr Addr, var3 Var) (result uint16, err error) {
	logger = comm.LogWithKeys(logger, "формат", "uint16")
	_, err = Read3(logger, responseReader, addr, var3, 1,
		func(_, response []byte) (string, error) {
			result = binary.LittleEndian.Uint16(response[3:5])
			return strconv.Itoa(int(result)), nil
		})
	return
}

func Read3BCD(logger *structlog.Logger, responseReader ResponseReader, addr Addr, var3 Var) (result float64, err error) {
	logger = comm.LogWithKeys(logger, "формат", "bcd")
	_, err = Read3(logger, responseReader, addr, var3, 2,
		func(request []byte, response []byte) (string, error) {
			var ok bool
			if result, ok = ParseBCD6(response[3:]); !ok {
				return "", merry.Errorf("не правильный код BCD % X", response[3:7])
			}
			return fmt.Sprintf("%v", result), nil
		})
	return
}

func Write32(logger *structlog.Logger, responseReader ResponseReader, addr Addr, protocolCommandCode ProtoCmd, deviceCommandCode DevCmd, value float64) error {

	logger = comm.LogWithKeys(logger,
		keyModbus, "запись_в_регистр_32",
		"команда", deviceCommandCode,
		"аргумент", value,
	)

	req := NewWrite32BCDRequest(addr, protocolCommandCode, deviceCommandCode, value)

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
