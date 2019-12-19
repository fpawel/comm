package comm

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm/internal"
	"github.com/powerman/structlog"
	"io"
	"sync"
	"time"
)

type Logger = *structlog.Logger

type ResponseParser = func(request, response []byte) (string, error)

type Config struct {
	TimeoutGetResponse time.Duration `json:"timeout_get_response" yaml:"timeout_get_response"` // таймаут получения ответа
	TimeoutEndResponse time.Duration `json:"timeout_end_response" yaml:"timeout_end_response"` // таймаут окончания ответа
	MaxAttemptsRead    int           `json:"max_attempts_read" yaml:"max_attempts_read"`       //число попыток получения ответа
	Pause              time.Duration `json:"pause" yaml:"pause"`                               //пауза перед опросом
}

var Err = merry.New("ошибка проткола последовательной приёмопередачи")

const (
	LogKeyDeviceValue = "device_value"
	LogKeyDuration    = "comm_duration"
	LogKeyAttempt     = "comm_attempt"
)

func GetResponse(log Logger, ctx context.Context, cfg Config, rw io.ReadWriter, prs ResponseParser, request []byte) ([]byte, error) {
	if cfg.MaxAttemptsRead < 1 {
		cfg.MaxAttemptsRead = 1
	}
	x := helper{
		rw:  rw,
		cfg: cfg,
		prs: prs,
		req: request,
	}
	response, result, err := x.getResponse(log, ctx)
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
		Appendf("timeout_get_response=%v", cfg.TimeoutGetResponse).
		Appendf("timeout_end_response=%v", cfg.TimeoutEndResponse).
		Appendf("max_attempts_read=%d", cfg.MaxAttemptsRead)
	if len(response) > 0 {
		err = merry.Appendf(err, "ответ=`% X`", response)
	}
	if len(result) > 0 {
		err = merry.Appendf(err, "результат=%s", result)
	}
	return response, err
}

type helper struct {
	rw  io.ReadWriter
	cfg Config
	prs ResponseParser
	req []byte
}

type result struct {
	response []byte
	err      error
}

func (x helper) getResponse(log Logger, ctx context.Context) ([]byte, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var (
		lastResult result
	)
	for attempt := 0; attempt < x.cfg.MaxAttemptsRead; attempt++ {
		if err := x.write(ctx, x.req); err != nil {
			return nil, "", err
		}
		ctx, _ := context.WithTimeout(ctx, x.cfg.TimeoutGetResponse)
		c := make(chan result)
		startWaitResponseMoment := time.Now()
		go x.waitForResponse(ctx, c)

		log := internal.LogPrependSuffixKeys(log, LogKeyAttempt, attempt)

		select {

		case r := <-c:
			strResult := ""
			if r.err == nil && x.prs != nil {
				strResult, r.err = x.prs(x.req, r.response)
			}
			if len(strResult) > 0 {
				log = internal.LogPrependSuffixKeys(log,
					LogKeyDeviceValue, strResult,
					LogKeyDuration, time.Since(startWaitResponseMoment))
			}
			x.logAnswer(log, r.response, r.err)
			if merry.Is(r.err, Err) {
				lastResult = r
				pause(ctx.Done(), x.cfg.TimeoutEndResponse)
				continue
			}
			if r.err != nil {
				return r.response, strResult, r.err
			}

			return r.response, strResult, nil

		case <-ctx.Done():

			x.logAnswer(log, nil, ctx.Err())

			switch ctx.Err() {

			case context.DeadlineExceeded:
				lastResult = result{
					response: nil,
					err:      ctx.Err(),
				}
				continue

			default:
				return nil, "", ctx.Err()
			}
		}
	}
	return lastResult.response, "", lastResult.err

}

func (x helper) write(ctx context.Context, request []byte) error {

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

func (x helper) waitForResponse(ctx context.Context, c chan result) {

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

func (x helper) read(bytesToReadCount int) ([]byte, error) {

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

type PrintfFunc = func(msg interface{}, keyvals ...interface{})

func SetEnableLog(enable bool) {
	mu.Lock()
	defer mu.Unlock()
	enableLog = enable
}

func (x helper) logAnswer(log Logger, response []byte, err error) {
	if !isLogEnabled() {
		return
	}
	str := fmt.Sprintf("% X --> % X", x.req, response)
	if len(response) == 0 {
		str = fmt.Sprintf("% X", x.req)
	}

	if err == nil {
		log.Info(str)
		return
	}
	if merry.Is(err, context.Canceled) {
		log.Warn(str + ": прервано")
		return
	}
	str += ": " + err.Error()
	stack := internal.FormatMerryStacktrace(err)
	if len(stack) > 0 {
		str += stack
	}
	log.PrintErr(str)
}

func isLogEnabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enableLog
}

var (
	mu        sync.Mutex
	enableLog = true
)

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
