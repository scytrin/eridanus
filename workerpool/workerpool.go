package workerpool

import (
	"context"
	"fmt"
	"path"
	"runtime"
	"time"

	ctxlogrus "github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
)

type JobFunc func(context.Context)

type WorkPool interface {
	Do(context.Context, JobFunc)
	WhenExhausted(context.CancelFunc)
	Close()
	Cap() int
	Len() int
	Busy() int
}

var NewPool = NewChannelPool

type taskItem struct {
	context.Context
	JobFunc
}

type taskQueue struct {
	WorkPool
	tasks chan taskItem
}

func NewTaskQueue(p WorkPool, max int) WorkPool {
	q := &taskQueue{p, make(chan taskItem, max)}
	go func() {
		for item := range q.tasks {
			q.WorkPool.Do(item.Context, item.JobFunc)
		}
	}()
	return q
}

func (q taskQueue) Do(ctx context.Context, job JobFunc) {
	q.tasks <- taskItem{ctx, job}
}

func (q taskQueue) Close() {
	defer close(q.tasks)
	q.WorkPool.Close()
}

type channelPool chan int

func NewChannelPool(max int) channelPool {
	if max < 1 {
		max = 1
	}
	p := make(channelPool, max)
	for i := 1; i <= max; i++ {
		p <- i
	}
	return p
}

func (p channelPool) Do(ctx context.Context, job JobFunc) {
	log := ctxlogrus.Extract(ctx)

	if _, f, l, ok := runtime.Caller(1); ok {
		// blank if called as `go pool.Do(...)`
		log = log.WithField("poolCall", fmt.Sprintf("%s:%d", path.Base(f), l))
	}

	log.Debug("waiting...")
	i := <-p
	go func() {
		log.Debugf("%d start", i)
		job(ctxlogrus.ToContext(ctx, log))
		log.Debugf("%d done", i)
		p <- i
		log.Debugf("%d released", i)
	}()
}

func (p channelPool) WhenExhausted(do context.CancelFunc) {
	bin := make(chan int, cap(p))
	defer func() {
		for v := range bin {
			p <- v
		}
	}()
	defer do()
	defer close(bin)

	for i := 0; i < cap(p); i++ {
		bin <- <-p
	}
}

func (p channelPool) Close() {
	defer close(p)
	for i := 0; i < cap(p); i++ {
		<-p
	}
}

func (p channelPool) Cap() int {
	return cap(p)
}

func (p channelPool) Len() int {
	return len(p)
}

func (p channelPool) Busy() int {
	return p.Cap() - p.Len()
}

func (p channelPool) TimedWait(ctx context.Context) {
	d := 1 * time.Second
	c := 0
	e := 3
	for {
		select {
		case <-ctx.Done():
		case <-time.After(d):
			if p.Busy() > 0 {
				c = 0
				continue
			}
			if c < e {
				c++
				continue
			}
		}
		// ctxlogrus.Extract(ctx).Infof("idle for more than %s, exiting",
		// 	time.Duration(c)*d)
		return
	}
}
