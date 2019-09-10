package comport

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/modbus"
	"github.com/fpawel/gohelp"
	"github.com/powerman/structlog"
	"time"
)

type Port struct {
	c func() Config
	p *winComport
}

func NewPort(getConfigFunc func() Config) *Port {
	return &Port{
		c: getConfigFunc,
	}
}

func (x *Port) NewResponseReader(ctx context.Context, cfg comm.Config) modbus.ResponseReader {
	return responseReader{
		Port: x,
		ctx:  ctx,
		cfg:  cfg,
	}
}

func (x *Port) Opened() bool {
	return x.p != nil
}

func (x *Port) Open() error {
	if x.p != nil {
		return nil
	}
	config := x.c()
	if config.ReadTimeout == 0 {
		config.ReadTimeout = time.Millisecond
	}
	var err error
	x.p, err = openWinComport(&config)
	return err
}

func (x *Port) Close() error {
	if x.p == nil {
		return nil
	}
	err := x.p.Close()
	x.p = nil
	return err
}

func (x *Port) Write(buf []byte) (int, error) {
	if err := x.Open(); err != nil {
		return 0, err
	}
	return x.p.Write(buf)
}

func (x *Port) Read(buf []byte) (int, error) {
	if err := x.Open(); err != nil {
		return 0, err
	}
	if len(buf) == 0 {
		return x.p.BytesToReadCount()
	}
	return x.p.Read(buf)
}

type responseReader struct {
	*Port
	ctx context.Context
	cfg comm.Config
}

func (x responseReader) GetResponse(request []byte, log comm.Logger, rp comm.ResponseParser) ([]byte, error) {
	cfg := x.c()
	log = logPrependSuffixKeys(log, "comport", fmt.Sprintf("%+v", cfg))
	b, err := comm.NewResponseReader(x.ctx, x.Port, x.cfg, rp).GetResponse(request, log)
	return b, merry.Appendf(err, "comport=%+v", cfg)
}

func logPrependSuffixKeys(log *structlog.Logger, a ...interface{}) *structlog.Logger {
	return gohelp.LogPrependSuffixKeys(log, a...)
}
