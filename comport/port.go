package comport

import (
	"github.com/ansel1/merry"
	"github.com/powerman/structlog"
	"time"
)

type Port struct {
	c Config
	p *port
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
		if log != nil {
			log.ErrIfFail(x.Close, "закрыть_порт", x.c.Name)
		}
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
	if len(x.c.Name) == 0 {
		return merry.New("не задано имя СОМ порта")
	}

	var err error
	x.p, err = openPort(&x.c)
	if err != nil {
		return merry.Prepend(err, x.c.Name)
	}
	return nil
}

func (x *Port) Close() error {
	if x.p == nil {
		return nil
	}
	err := x.p.Close()
	x.p = nil
	if err != nil {
		return merry.Prependf(err, "%s: закрыть", x)
	}
	return nil
}

func (x *Port) Write(buf []byte) (int, error) {
	if err := x.Open(); err != nil {
		return 0, err
	}
	n, err := x.p.Write(buf)
	if err != nil {
		err = merry.Prependf(err, "%s: запись", x)
	}
	return n, err
}

func (x *Port) Read(buf []byte) (int, error) {
	if err := x.Open(); err != nil {
		return 0, err
	}
	n, err := readPort(x.p, buf)
	if err != nil {
		err = merry.Prependf(err, "%s: считывание", x)
	}
	return n, err
}

func (x *Port) String() string {
	if len(x.c.Name) > 0 {
		return x.c.Name
	}
	return "СОМ?"
}

func readPort(p *port, buf []byte) (int, error) {
	if len(buf) == 0 {
		return p.BytesToReadCount()
	}
	return p.Read(buf)
}
