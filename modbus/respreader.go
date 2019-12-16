package modbus

import (
	"context"
	"github.com/fpawel/comm"
	"github.com/fpawel/comm/comport"
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
	return comm.NewResponseReader(x.p, x.c, rp).GetResponse(log, ctx, request)
}
