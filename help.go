package comm

import "github.com/powerman/structlog"

func LogWithKeys(logger *structlog.Logger, args ...interface{}) *structlog.Logger {
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
	return logger.New(args...).PrependSuffixKeys(keys...)
}

func NewLogWithKeys(args ...interface{}) *structlog.Logger {
	return LogWithKeys(structlog.New(), args...)
}
