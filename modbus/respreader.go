package modbus

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/comport"
	"github.com/fpawel/comm/internal"
)

type ResponseReader interface {
	GetResponse(comm.Logger, context.Context, []byte, comm.ResponseParser) ([]byte, error)
}

func NewResponseReader(p *comport.Port, c comm.Config) ResponseReader {
	return comportResponseReader{p: p, c: c}
}

type comportResponseReader struct {
	p *comport.Port
	c comm.Config
}

func (x comportResponseReader) GetResponse(log comm.Logger, ctx context.Context, request []byte, rp comm.ResponseParser) ([]byte, error) {
	log = internal.LogPrependSuffixKeys(log, "comport", fmt.Sprintf("%+v", x.c))
	b, err := comm.NewResponseReader(x.p, x.c, rp).GetResponse(log, ctx, request)
	return b, merry.Appendf(err, "comport=%+v", x.c)
}
