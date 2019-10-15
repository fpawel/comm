package internal

import (
	"bytes"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/powerman/structlog"
	"path/filepath"
	"runtime"
)

func LogPrependSuffixKeys(log comm.Logger, args ...interface{}) *structlog.Logger {
	var keys []string
	for i, arg := range args {
		if i%2 == 0 {
			k, ok := arg.(string)
			if !ok {
				panic("key must be string")
			}
			keys = append(keys, k)
		}
	}
	return log.New(args...).PrependSuffixKeys(keys...)
}

// FormatMerryStacktrace returns the error's stacktrace as a string formatted
// the same way as golangs runtime package.
// If e has no stacktrace, returns an empty string.
func FormatMerryStacktrace(e error) string {
	s := merry.Stack(e)
	if len(s) == 0 {
		return ""
	}
	buf := bytes.Buffer{}
	for i, fp := range s {
		fnc := runtime.FuncForPC(fp)
		if fnc != nil {
			f, l := fnc.FileLine(fp)
			name := filepath.Base(fnc.Name())
			ident := " "
			if i > 0 {
				ident = "\t"
			}
			buf.WriteString(fmt.Sprintf("%s%s:%d %s\n", ident, f, l, name))
		}
	}
	return buf.String()

}
