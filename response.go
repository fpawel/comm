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

type ResponseReader struct {
	ReadWriter     io.ReadWriter
	Config         Config
	ResponseParser ResponseParser
}

func NewResponseReader(readWriter io.ReadWriter, config Config, responseParser ResponseParser) ResponseReader {
	return ResponseReader{
		ReadWriter:     readWriter,
		Config:         config,
		ResponseParser: responseParser,
	}
}

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

func (x ResponseReader) GetResponse(log Logger, ctx context.Context, request []byte) ([]byte, error) {
	if x.Config.MaxAttemptsRead < 1 {
		x.Config.MaxAttemptsRead = 1
	}
	response, result, err := x.getResponse(log, ctx, request)
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
	err = merry.Appendf(err, "запрорс=`% X`", request).Appendf("comm=%+v", x.Config)
	if len(response) > 0 {
		err = merry.Appendf(err, "ответ=`% X`", response)
	}
	if len(result) > 0 {
		err = merry.Appendf(err, "результат=%s", result)
	}
	return response, err
}

type result struct {
	response []byte
	err      error
}

func (x ResponseReader) getResponse(log Logger, ctx context.Context, request []byte) ([]byte, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var (
		lastResult result
	)
	for attempt := 0; attempt < x.Config.MaxAttemptsRead; attempt++ {
		if err := x.write(ctx, request); err != nil {
			return nil, "", err
		}
		ctx, _ := context.WithTimeout(ctx, x.Config.TimeoutGetResponse)
		c := make(chan result)
		startWaitResponseMoment := time.Now()
		go x.waitForResponse(ctx, c)

		log := internal.LogPrependSuffixKeys(log, LogKeyAttempt, attempt)

		select {

		case r := <-c:
			strResult := ""
			if r.err == nil && x.ResponseParser != nil {
				strResult, r.err = x.ResponseParser(request, r.response)
			}
			if len(strResult) > 0 {
				log = internal.LogPrependSuffixKeys(log,
					LogKeyDeviceValue, strResult,
					LogKeyDuration, time.Since(startWaitResponseMoment))
			}
			logAnswer(log, request, r.response, r.err)
			if merry.Is(r.err, Err) {
				lastResult = r
				time.Sleep(x.Config.TimeoutEndResponse)
				continue
			}
			if r.err != nil {
				return r.response, strResult, r.err
			}

			return r.response, strResult, nil

		case <-ctx.Done():

			logAnswer(log, request, nil, ctx.Err())

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

func (x ResponseReader) write(ctx context.Context, request []byte) error {

	if x.Config.Pause > 0 {
		pause(ctx.Done(), x.Config.Pause)
	}

	t := time.Now()
	writtenCount, err := x.ReadWriter.Write(request)
	for ; err == nil && writtenCount == 0 &&
		time.Since(t) < x.Config.TimeoutGetResponse; writtenCount, err = x.ReadWriter.Write(request) {
		// COMPORT PENDING
		time.Sleep(x.Config.TimeoutEndResponse)
	}
	if err != nil {
		return merry.Wrap(err)
	}
	if writtenCount != len(request) {
		return fmt.Errorf("записано %d байт из %d", writtenCount, len(request))
	}
	return err
}

func (x ResponseReader) waitForResponse(ctx context.Context, c chan result) {

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
			bytesToReadCount, err := x.ReadWriter.Read(nil)
			if err != nil {
				c <- result{response, merry.Wrap(err)}
				return
			}
			if bytesToReadCount == 0 {
				time.Sleep(time.Millisecond)
				continue
			}
			b, err := x.read(bytesToReadCount)
			if err != nil {
				c <- result{response, merry.Wrap(err)}
				return
			}
			response = append(response, b...)
			ctx = context.Background()
			ctxReady, _ = context.WithTimeout(context.Background(), x.Config.TimeoutEndResponse)
		}
	}
}

func (x ResponseReader) read(bytesToReadCount int) ([]byte, error) {

	b := make([]byte, bytesToReadCount)
	readCount, err := x.ReadWriter.Read(b)
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

func logAnswer(log Logger, request, response []byte, err error) {
	if !isLogEnabled() {
		return
	}
	str := fmt.Sprintf("% X --> % X", request, response)
	if len(response) == 0 {
		str = fmt.Sprintf("% X", request)
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
