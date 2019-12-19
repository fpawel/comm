package comport

import (
	"context"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/powerman/structlog"
	"time"
)

type Port struct {
	c Config
	p *winComport
}

type ResponseReader interface {
	GetResponse(comm.Logger, context.Context, []byte) ([]byte, error)
}

type ResponseReadParser interface {
	GetResponse(comm.Logger, context.Context, []byte, comm.ResponseParser) ([]byte, error)
}

func NewPort(c Config) *Port {
	return &Port{c: c}
}

// Config возвращает параметры СОМ порта
func (x *Port) Config() Config {
	return x.c
}

// SetConfig устанавливае параметры СОМ порта
func (x *Port) SetConfig(c Config) error {
	if c.ReadTimeout == 0 {
		c.ReadTimeout = time.Millisecond
	}
	if x.c == c {
		return nil
	}
	if x.p != nil {
		if err := x.Close(); err != nil {
			return err
		}
	}
	x.c = c
	return nil
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

func (x *Port) ResponseReadParser(c comm.Config) ResponseReadParser {
	return simpleResponseReadParser{p: x, c: c}
}

func (x *Port) ResponseReader(c comm.Config) ResponseReader {
	return simpleResponseReader{p: x, c: c}
}

type simpleResponseReader struct {
	p *Port
	c comm.Config
}

func (x simpleResponseReader) GetResponse(log *structlog.Logger, ctx context.Context, req []byte) ([]byte, error) {
	return comm.GetResponse(log, ctx, x.c, x.p, nil, req)
}

type simpleResponseReadParser struct {
	p *Port
	c comm.Config
}

func (x simpleResponseReadParser) GetResponse(log comm.Logger, ctx context.Context, request []byte, rp comm.ResponseParser) ([]byte, error) {
	return comm.GetResponse(log, ctx, x.c, x.p, rp, request)
}
