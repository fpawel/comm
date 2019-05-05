package comport

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/powerman/structlog"
	"time"
)

type Reader struct {
	config Config
	port   *Port
}

func NewReader(config Config) *Reader {
	if config.ReadTimeout == 0 {
		config.ReadTimeout = time.Millisecond
	}
	return &Reader{
		config: config,
		//logger: structlog.New("device", device).AppendPrefixKeys("device"),
	}
}

func (x *Reader) Config() Config {
	return x.config
}

func (x *Reader) Opened() bool {
	return x.port != nil
}

func (x *Reader) Open(name string) error {
	if x.Opened() {
		return merry.New("already opened")
	}
	x.config.Name = name
	port, err := OpenPort(&x.config)
	if err != nil {
		return merry.Append(err, name)
	}
	x.port = port
	return nil
}

func (x *Reader) Close() error {
	if x.port == nil {
		return nil
	}
	err := x.port.Close()
	x.port = nil
	return err
}

func (x *Reader) OpenDebounce(name string, bounceTimeout time.Duration, ctx context.Context) error {
	if x.Opened() {
		return merry.New("already opened")
	}
	x.config.Name = name
	port, err := OpenPortDebounce(&x.config, bounceTimeout, ctx)
	if err == nil {
		x.port = port
	}
	return err
}

func (x *Reader) GetResponse(request comm.Request, ctx context.Context) ([]byte, error) {
	request.Logger = withKeyValue(request.Logger, "port", fmt.Sprintf("%s,%d", x.config.Name, x.config.Baud))
	request.ReadWriter = x.port
	return comm.GetResponse(request, ctx)
}

func (x *Reader) Write(buf []byte) (int, error) {
	return x.port.Write(buf)
}

func withKeyValue(logger *structlog.Logger, key string, value interface{}) *structlog.Logger {
	return logger.New(key, value).PrependSuffixKeys(key)
}
