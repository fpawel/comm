package comm

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm/internal"
	"github.com/powerman/structlog"
	"io"
	"sync/atomic"
	"time"
)

type Logger = *structlog.Logger

type ParseResponseFunc = func(request, response []byte) error

type NotifyFunc = func(Info)

type Info struct {
	Request  []byte
	Response []byte
	Err      error
	Duration time.Duration
	Port     string
}

type Config struct {
	TimeoutGetResponse time.Duration `json:"timeout_get_response" yaml:"timeout_get_response"` // таймаут получения ответа
	TimeoutEndResponse time.Duration `json:"timeout_end_response" yaml:"timeout_end_response"` // таймаут окончания ответа
	MaxAttemptsRead    int           `json:"max_attempts_read" yaml:"max_attempts_read"`       //число попыток получения ответа
	Pause              time.Duration `json:"pause" yaml:"pause"`                               //пауза перед опросом
}

var Err = merry.New("ошибка проткола последовательной приёмопередачи")

const (
	LogKeyDuration = "comm_duration"
	LogKeyAttempt  = "comm_attempt"
)

type T struct {
	cfg Config
	rw  io.ReadWriter
	prs ParseResponseFunc
}

func New(rw io.ReadWriter, cfg Config) T {
	if cfg.MaxAttemptsRead < 1 {
		cfg.MaxAttemptsRead = 1
	}
	return T{
		cfg: cfg,
		rw:  rw,
	}
}

func (x T) WithReadWriter(rw io.ReadWriter) T {
	x.rw = rw
	return x
}

func (x T) WithConfig(cfg Config) T {
	x.cfg = cfg
	return x
}

func (x T) WithAppendParse(prs ParseResponseFunc) T {
	xPrs := x.prs
	x.prs = func(request, response []byte) error {
		if xPrs != nil {
			if err := xPrs(request, response); err != nil {
				return err
			}
		}
		if err := prs(request, response); err != nil {
			return err
		}
		return nil
	}
	return x
}

func (x T) WithPrependParse(prs ParseResponseFunc) T {
	xPrs := x.prs
	x.prs = func(request, response []byte) error {
		if err := prs(request, response); err != nil {
			return err
		}
		if xPrs != nil {
			if err := xPrs(request, response); err != nil {
				return err
			}
		}
		return nil
	}
	return x
}

func (x T) GetResponse(log Logger, ctx context.Context, request []byte) ([]byte, error) {

	response, err := x.getResponse(log, ctx, request)
	if err == nil {
		return response, nil
	}
	if merry.Is(err, context.Canceled) {
		err = merry.Append(err, "прервано")
	} else if merry.Is(err, context.DeadlineExceeded) {
		err = merry.WithMessage(err, "нет ответа").WithCause(Err)
		if !merry.Is(err, Err) || !merry.Is(err, context.DeadlineExceeded) {
			panic("unexpected")
		}
	}
	err = merry.Appendf(err, "запрорс=`% X`", request).
		Appendf("timeout_get_response=%v", x.cfg.TimeoutGetResponse).
		Appendf("timeout_end_response=%v", x.cfg.TimeoutEndResponse).
		Appendf("max_attempts_read=%d", x.cfg.MaxAttemptsRead)
	if len(response) > 0 {
		err = merry.Appendf(err, "ответ=`% X`", response)
	}
	return response, err
}

func SetEnableLog(enable bool) {
	if enable {
		atomic.StoreInt32(&atomicEnableLog, 1)
	} else {
		atomic.StoreInt32(&atomicEnableLog, 0)
	}
}

func SetNotify(f NotifyFunc) {
	atomicNotify.Store(f)
}

type result struct {
	response []byte
	err      error
}

func (x T) getResponse(log Logger, ctx context.Context, request []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var (
		lastResult result
	)
	for attempt := 0; attempt < x.cfg.MaxAttemptsRead; attempt++ {
		if err := x.write(ctx, request); err != nil {
			return nil, err
		}
		ctx, _ := context.WithTimeout(ctx, x.cfg.TimeoutGetResponse)
		c := make(chan result)
		startWaitResponseMoment := time.Now()
		go x.waitForResponse(ctx, c)

		log := internal.LogPrependSuffixKeys(log, LogKeyAttempt, attempt)

		select {

		case r := <-c:
			if r.err == nil && x.prs != nil {
				r.err = x.prs(request, r.response)
			}
			log = internal.LogPrependSuffixKeys(log, LogKeyDuration, time.Since(startWaitResponseMoment))
			logAnswer(log, request, r)
			notify(startWaitResponseMoment, request, r, x.rw)

			if merry.Is(r.err, Err) {
				lastResult = r
				pause(ctx.Done(), x.cfg.TimeoutEndResponse)
				continue
			}
			if r.err != nil {
				return r.response, r.err
			}

			return r.response, nil

		case <-ctx.Done():

			logAnswer(log, request, result{
				response: nil,
				err:      ctx.Err(),
			})

			switch ctx.Err() {

			case context.DeadlineExceeded:
				lastResult = result{
					response: nil,
					err:      ctx.Err(),
				}
				continue

			default:
				return nil, ctx.Err()
			}
		}
	}
	return lastResult.response, lastResult.err
}

func (x T) write(ctx context.Context, request []byte) error {

	if x.cfg.Pause > 0 {
		pause(ctx.Done(), x.cfg.Pause)
	}

	t := time.Now()
	writtenCount, err := x.rw.Write(request)
	for ; err == nil && writtenCount == 0 &&
		time.Since(t) < x.cfg.TimeoutGetResponse; writtenCount, err = x.rw.Write(request) {
		// COMPORT PENDING
		pause(ctx.Done(), x.cfg.TimeoutEndResponse)
	}
	if err != nil {
		return merry.Wrap(err)
	}
	if writtenCount != len(request) {
		return fmt.Errorf("записано %d байт из %d", writtenCount, len(request))
	}
	return err
}

func (x T) waitForResponse(ctx context.Context, c chan result) {

	var response []byte
	ctxReady := context.Background()

	for {
		select {

		case <-ctx.Done():
			return

		case <-ctxReady.Done():
			c <- result{response, nil}
			return

		default:
			bytesToReadCount, err := x.rw.Read(nil)
			if err != nil {
				c <- result{response, merry.Wrap(err)}
				return
			}
			if bytesToReadCount == 0 {
				pause(ctx.Done(), time.Millisecond)
				continue
			}
			b, err := x.read(bytesToReadCount)
			if err != nil {
				c <- result{response, merry.Wrap(err)}
				return
			}
			response = append(response, b...)
			ctx = context.Background()
			ctxReady, _ = context.WithTimeout(context.Background(), x.cfg.TimeoutEndResponse)
		}
	}
}

func (x T) read(bytesToReadCount int) ([]byte, error) {

	b := make([]byte, bytesToReadCount)
	readCount, err := x.rw.Read(b)
	if err != nil {
		return nil, merry.Wrap(err)
	}
	if readCount != bytesToReadCount {
		return nil, fmt.Errorf("считано %d байт из %d: % X", readCount, bytesToReadCount, b[:readCount])
	}
	return b, nil
}

func logAnswer(log Logger, request []byte, r result) {
	if !isLogEnabled() {
		return
	}
	str := fmt.Sprintf("% X --> % X", request, r.response)
	if len(r.response) == 0 {
		str = fmt.Sprintf("% X", request)
	}

	if r.err == nil {
		log.Info(str)
		return
	}
	if merry.Is(r.err, context.Canceled) {
		log.Warn(str + ": прервано")
		return
	}
	str += ": " + r.err.Error()
	stack := internal.FormatMerryStacktrace(r.err)
	if len(stack) > 0 {
		str += stack
	}
	log.PrintErr(str)
}

func isLogEnabled() bool {
	return atomic.LoadInt32(&atomicEnableLog) != 0
}

func notify(startWaitResponseMoment time.Time, req []byte, r result, rw io.ReadWriter) {
	ntf := getNotifyFunc()
	if ntf == nil {
		return
	}
	i := Info{
		Request:  make([]byte, len(req)),
		Response: make([]byte, len(r.response)),
		Err:      r.err,
		Duration: time.Since(startWaitResponseMoment),
	}
	if s, f := rw.(fmt.Stringer); f {
		i.Port = s.String()
	}
	copy(i.Request, req)
	copy(i.Response, r.response)
	go ntf(i)
}

func getNotifyFunc() NotifyFunc {
	x := atomicNotify.Load()
	if x == nil {
		return nil
	}
	return x.(NotifyFunc)
}

func pause(chDone <-chan struct{}, d time.Duration) {
	timer := time.NewTimer(d)
	for {
		select {
		case <-timer.C:
			return
		case <-chDone:
			timer.Stop()
			return
		}
	}
}

var (
	atomicEnableLog int32 = 1
	atomicNotify          = new(atomic.Value)
)
