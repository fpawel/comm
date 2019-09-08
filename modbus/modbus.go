package modbus

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/powerman/structlog"
	"strconv"
)

const (
	LogKeyAddr         = "modbus_address"
	LogKeyCmd          = "modbus_cmd"
	LogKeyData         = "modbus_data"
	LogKeyRegsCount    = "modbus_regs_count"
	LogKeyFirstReg     = "modbus_first_register"
	LogKeyDeviceCmd    = "modbus_device_comd"
	LogKeyDeviceCmdArg = "modbus_device_cmd_arg"
)

func SetLogKeysFormat() {
	structlog.DefaultLogger.SetKeysFormat(
		map[string]string{
			LogKeyData: " %[1]s=`% [2]X`",
		})
}

func Read3(log comm.Logger, ctx context.Context,
	responseReader ResponseReader, addr Addr,
	firstReg Var, regsCount uint16,
	parseResponse comm.ResponseParser) ([]byte, error) {

	log = logPrependSuffixKeys(log,
		LogKeyRegsCount, regsCount,
		LogKeyFirstReg, firstReg,
	)

	req := Request{
		Addr:     addr,
		ProtoCmd: 3,
		Data:     append(uint16b(uint16(firstReg)), uint16b(regsCount)...),
	}

	b, err := req.GetResponse(log, ctx, responseReader, func(request, response []byte) (string, error) {
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

func Read3BCDs(log comm.Logger, ctx context.Context, responseReader ResponseReader, addr Addr, var3 Var, count int) ([]float64, error) {
	//log = logPrependSuffixKeys(log, "format", "BCD", "values_count", count)
	var values []float64
	_, err := Read3(log, ctx, responseReader, addr, var3, uint16(count*2),
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

func Read3UInt16(log comm.Logger, ctx context.Context, responseReader ResponseReader, addr Addr, var3 Var) (uint16, error) {
	//log = logPrependSuffixKeys(log, "format", "uint16")
	var result uint16
	_, err := Read3(log, ctx, responseReader, addr, var3, 1,
		func(_, response []byte) (string, error) {
			result = binary.LittleEndian.Uint16(response[3:5])
			return strconv.Itoa(int(result)), nil
		})
	return result, merry.Append(err, "запрос числа в uin16")
}

func Read3BCD(log comm.Logger, ctx context.Context, responseReader ResponseReader, addr Addr, var3 Var) (float64, error) {
	//log = logPrependSuffixKeys(log, "format", "bcd")
	var result float64
	_, err := Read3(log, ctx, responseReader, addr, var3, 2,
		func(request []byte, response []byte) (string, error) {
			var ok bool
			if result, ok = ParseBCD6(response[3:]); !ok {
				return "", comm.Err.Here().Appendf("не правильный код BCD % X", response[3:7])
			}
			return fmt.Sprintf("%v", result), nil
		})
	return result, merry.Append(err, "запрос числа в BCD")
}

func Write32(log comm.Logger, ctx context.Context,
	responseReader ResponseReader, addr Addr, protocolCommandCode ProtoCmd,
	deviceCommandCode DevCmd, value float64) error {
	log = logPrependSuffixKeys(log,
		LogKeyDeviceCmd, deviceCommandCode,
		LogKeyDeviceCmdArg, value,
	)
	req := NewWrite32BCDRequest(addr, protocolCommandCode, deviceCommandCode, value)
	_, err := req.GetResponse(log, ctx, responseReader, func(request, response []byte) (string, error) {
		for i := 2; i < 6; i++ {
			if request[i] != response[i] {
				return "", Err.Here().
					WithMessagef("ошибка формата: запрос[2:6]==[% X] != ответ[2:6]==[% X]", request[2:6], response[2:6])
			}
		}
		return "OK", nil
	})
	return merry.Appendf(err, "запись в 32-ой регистр команды uint16 %X с аргументом BCD %v",
		deviceCommandCode, value)
}
