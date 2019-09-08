package modbus

import (
	"github.com/fpawel/comm"
	"github.com/fpawel/gohelp"
	"github.com/powerman/structlog"
)

func uint16b(v uint16) (b []byte) {
	b = make([]byte, 2)
	b[0] = byte(v >> 8)
	b[1] = byte(v)
	return
}

func logPrependSuffixKeys(log comm.Logger, a ...interface{}) *structlog.Logger {
	return gohelp.LogPrependSuffixKeys(log, a...)
}

//func logAppendPrefixKeys(log comm.Logger, a ...interface{}) *structlog.Logger{
//	return gohelp.LogAppendPrefixKeys(log, a...)
//}
