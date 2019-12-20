package comport

import (
	"github.com/ansel1/merry"
	"github.com/powerman/structlog"
	"time"
)

type Port struct {
	c Config
	p *winComport
}

func NewPort(c Config) *Port {
	return &Port{c: c}
}

// Config возвращает параметры СОМ порта
func (x *Port) Config() Config {
	return x.c
}

// SetConfig устанавливае параметры СОМ порта
func (x *Port) SetConfig(log *structlog.Logger, c Config) {
	if c.ReadTimeout == 0 {
		c.ReadTimeout = time.Millisecond
	}
	if x.c == c {
		return
	}
	if x.p != nil {
		log.ErrIfFail(x.Close, "close_comport", x.c.Name)
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
