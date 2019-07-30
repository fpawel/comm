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

func (x *ReadWriter) Open(log *structlog.Logger, ctx context.Context) error {
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

func (x *ReadWriter) logWrap(log *structlog.Logger) *structlog.Logger {
	cfg := x.getConfigFunc()
	return gohelp.LogPrependSuffixKeys(log, "comport", fmt.Sprintf("%s,%d", cfg.Name, cfg.Baud))
}

func (x *ReadWriter) GetResponse(log *structlog.Logger, ctx context.Context, requestBytes []byte, respParser comm.ResponseParser) ([]byte, error) {
	b, err := comm.GetResponse(log, ctx, x, comm.Request{
		Config:         x.getCommConfigFunc(),
		Bytes:          requestBytes,
		ResponseParser: respParser,
	})
	return b, merry.Append(err, x.getConfigFunc().String())
}

func (x *ReadWriter) Write(log *structlog.Logger, ctx context.Context, buf []byte) (int, error) {
	log = x.logWrap(log)
	if err := x.Open(log, ctx); err != nil {
		return 0, err
	}
	return x.port.Write(log, buf)
}

func (x *ReadWriter) Read(log *structlog.Logger, ctx context.Context, buf []byte) (int, error) {
	log = x.logWrap(log)
	if err := x.Open(log, ctx); err != nil {
		return 0, err
	}
	return x.port.Read(log, buf)
}

func (x *ReadWriter) BytesToReadCount() (int, error) {
	return x.port.BytesToReadCount()
}
