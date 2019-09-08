package comport

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/fpawel/gohelp"
	"github.com/powerman/structlog"
	"time"
)

type ReadWriter struct {
	getConfigFunc     func() Config
	getCommConfigFunc func() comm.Config
	port              *Port
	bounceTimeout     time.Duration
}

func NewReadWriter(getConfigFunc func() Config, getCommConfigFunc func() comm.Config) *ReadWriter {
	return &ReadWriter{
		getConfigFunc:     getConfigFunc,
		getCommConfigFunc: getCommConfigFunc,
	}
}

func (x *ReadWriter) SetBounceTimeout(bounceTimeout time.Duration) {
	x.bounceTimeout = bounceTimeout
}

func (x *ReadWriter) Opened() bool {
	return x.port != nil
}

func (x *ReadWriter) Open(ctx context.Context) error {
	if x.port != nil {
		return nil
	}
	config := x.getConfigFunc()
	if config.ReadTimeout == 0 {
		config.ReadTimeout = time.Millisecond
	}
	var err error
	if x.bounceTimeout == 0 {
		x.port, err = OpenPort(&config)
	} else {
		x.port, err = OpenPortDebounce(&config, x.bounceTimeout, ctx)
	}
	return err
}

func (x *ReadWriter) Close() error {
	if x.port == nil {
		return nil
	}
	err := x.port.Close()
	x.port = nil
	return err
}

func (x *ReadWriter) GetResponse(log *structlog.Logger, ctx context.Context, requestBytes []byte, respParser comm.ResponseParser) ([]byte, error) {
	cfg := x.getConfigFunc()
	log = logPrependSuffixKeys(log, "comport", fmt.Sprintf("%s,%d", cfg.Name, cfg.Baud))
	b, err := comm.GetResponse(log, ctx, x, comm.Request{
		Config:         x.getCommConfigFunc(),
		Bytes:          requestBytes,
		ResponseParser: respParser,
	})
	return b, merry.Appendf(err, "%+v", x.getConfigFunc())
}

func (x *ReadWriter) Write(ctx context.Context, buf []byte) (int, error) {
	if err := x.Open(ctx); err != nil {
		return 0, err
	}
	return x.port.Write(buf)
}

func (x *ReadWriter) Read(ctx context.Context, buf []byte) (int, error) {
	if err := x.Open(ctx); err != nil {
		return 0, err
	}
	return x.port.Read(buf)
}

func (x *ReadWriter) BytesToReadCount() (int, error) {
	return x.port.BytesToReadCount()
}

func logPrependSuffixKeys(log *structlog.Logger, a ...interface{}) *structlog.Logger {
	return gohelp.LogPrependSuffixKeys(log, a...)
}
