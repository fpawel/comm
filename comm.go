package comm

import (
	"context"
	"fmt"
	"github.com/ansel1/merry"
	"github.com/fpawel/comm/internal"
	"github.com/powerman/structlog"
	"io"
	"sync"
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
	Attempt  int
}

type Config struct {
	TimeoutGetResponse time.Duration `json:"timeout_get_response" yaml:"timeout_get_response"` // таймаут получения ответа
	TimeoutEndResponse time.Duration `json:"timeout_end_response" yaml:"timeout_end_response"` // таймаут окончания ответа
	MaxAttemptsRead    int           `json:"max_attempts_read" yaml:"max_attempts_read"`       //число попыток получения ответа
	Pause              time.Duration `json:"pause" yaml:"pause"`                               //пауза перед опросом
}

var Err = merry.New("ошибка проткола последовательной приёмопередачи")

const (
	LogKeyDuration = "время_ответа"
	LogKeyAttempt  = "число_попыток"
)

type T struct {
	cfg  Config
	rw   io.ReadWriter
	prs  ParseResponseFunc
	port string
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

func (x T) WithLockPort(port string) T {
	x.port = port
	return x
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

	x.lockPort()
	response, err := x.getResponse(log, ctx, request)
	x.unlockPort()

	if err == nil {
		return response, nil
	}
	if merry.Is(err, context.Canceled) {
		err = merry.Append(err, "ожидание ответа прервано")
	} else if merry.Is(err, context.DeadlineExceeded) {
		err = merry.WithMessage(err, "нет ответа").WithCause(Err)
		if !merry.Is(err, Err) || !merry.Is(err, context.DeadlineExceeded) {
			panic("unexpected")
		}
	}
	err = merry.Appendf(err, "запрорс % X", request).
		Appendf("таймаут ожидания ответа %v, таймаут окончания ответа %v, %d повторов",
			x.cfg.TimeoutGetResponse,
			x.cfg.TimeoutEndResponse,
			x.cfg.MaxAttemptsRead)
	if len(response) > 0 {
		err = merry.Appendf(err, "ответ % X", response)
	}
	return response, err
}

func Write(ctx context.Context, request []byte, rw io.Writer, cfg Config) error {

	t := time.Now()
	writtenCount, err := rw.Write(request)
	for ; err == nil && writtenCount == 0 &&
		time.Since(t) < cfg.TimeoutGetResponse; writtenCount, err = rw.Write(request) {
		// COMPORT PENDING
		pause(ctx.Done(), cfg.TimeoutEndResponse)
	}
	if err != nil {
		return merry.Wrap(err)
	}
	if writtenCount != len(request) {
		return merry.Errorf("записано %d байт из %d", writtenCount, len(request))
	}
	return err
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
			notify(startWaitResponseMoment, request, r, x.rw, attempt)

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
			r := result{
				response: nil,
				err:      ctx.Err(),
			}

			logAnswer(log, request, r)
			notify(startWaitResponseMoment, request, r, x.rw, attempt)

			switch ctx.Err() {

			case context.DeadlineExceeded:
				lastResult = r
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
	return Write(ctx, request, x.rw, x.cfg)
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
		return nil, merry.Errorf("считано %d байт из %d: % X", readCount, bytesToReadCount, b[:readCount])
	}
	return b, nil
}

func logAnswer(log Logger, request []byte, r result) {
	if log == nil || !isLogEnabled() {
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

func notify(startWaitResponseMoment time.Time, req []byte, r result, rw io.ReadWriter, attempt int) {
	ntf := getNotifyFunc()
	if ntf == nil {
		return
	}
	i := Info{
		Request:  make([]byte, len(req)),
		Response: make([]byte, len(r.response)),
		Err:      r.err,
		Duration: time.Since(startWaitResponseMoment),
		Attempt:  attempt,
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

func (x T) lockPort() {
	if len(x.port) == 0 {
		return
	}
	o, _ := lockPorts.LoadOrStore(x.port, new(sync.Mutex))
	mu, ok := o.(*sync.Mutex)
	if !ok {
		panic("unexpected")
	}
	mu.Lock()
}

func (x T) unlockPort() {
	if len(x.port) == 0 {
		return
	}
	o, ok := lockPorts.Load(x.port)
	if !ok {
		panic("unlock not locked port: " + x.port)
	}
	mu, ok := o.(*sync.Mutex)
	if !ok {
		panic("unexpected")
	}
	mu.Unlock()
}

var (
	atomicEnableLog int32 = 1
	atomicNotify          = new(atomic.Value)
	lockPorts       sync.Map
)
