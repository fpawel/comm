package comport

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/internal"
	"github.com/fpawel/comm/modbus"
	"time"
)

type Port struct {
	c Config
	p *winComport
}

func NewPort(c Config) *Port {
	return &Port{c: c}
}

func (x *Port) NewResponseReader(ctx context.Context, cfg comm.Config) modbus.ResponseReader {
	return responseReader{
		Port: x,
		ctx:  ctx,
		cfg:  cfg,
	}
}

// Config возвращает параметры СОМ порта
func (x *Port) Config() Config {
	return x.c
}

// SetConfig устанавливае параметры СОМ порта
func (x *Port) SetConfig(c Config) {
	if c.ReadTimeout == 0 {
		c.ReadTimeout = time.Millisecond
	}
	x.c = c
}

func (x *Port) Opened() bool {
	return x.p != nil
}

func (x *Port) Open() error {
	if x.p != nil {
		return nil
	}
	c0 := Config{}
	if x.c == c0 {
		return merry.New("параметры СОМ порта не были заданы")
	}

	var err error
	x.p, err = openWinComport(&x.c)
	if err != nil {
		return err
	}
	return nil
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
	log = internal.LogPrependSuffixKeys(log, "comport", fmt.Sprintf("%+v", x.c))
	b, err := comm.NewResponseReader(x.ctx, x.Port, x.cfg, rp).GetResponse(request, log)
	return b, merry.Appendf(err, "comport=%+v", x.c)
}
