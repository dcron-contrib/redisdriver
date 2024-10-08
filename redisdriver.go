package redisdriver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dcron-contrib/commons"
	"github.com/dcron-contrib/commons/dlog"
	redis "github.com/redis/go-redis/v9"
)

const (
	redisDefaultTimeout = 5 * time.Second
)

type RedisDriver struct {
	c           redis.UniversalClient
	serviceName string
	nodeID      string
	timeout     time.Duration
	logger      dlog.Logger
	started     bool

	// this context is used to define
	// the lifetime of this driver.
	runtimeCtx    context.Context
	runtimeCancel context.CancelFunc

	sync.Mutex
}

func NewDriver(redisClient redis.UniversalClient) *RedisDriver {
	rd := &RedisDriver{
		c: redisClient,
		logger: &dlog.StdLogger{
			Log: log.Default(),
		},
		timeout: redisDefaultTimeout,
	}
	rd.started = false
	return rd
}

func (rd *RedisDriver) Init(serviceName string, opts ...commons.Option) {
	rd.serviceName = serviceName
	rd.nodeID = commons.GetNodeId(rd.serviceName)

	for _, opt := range opts {
		rd.WithOption(opt)
	}
}

func (rd *RedisDriver) NodeID() string {
	return rd.nodeID
}

func (rd *RedisDriver) Start(ctx context.Context) (err error) {
	rd.Lock()
	defer rd.Unlock()
	if rd.started {
		err = errors.New("this driver is started")
		return
	}
	rd.runtimeCtx, rd.runtimeCancel = context.WithCancel(context.TODO())
	rd.started = true
	// register
	err = rd.registerServiceNode()
	if err != nil {
		rd.logger.Errorf("register service error=%v", err)
		return
	}
	// heartbeat timer
	go rd.heartBeat()
	return
}

func (rd *RedisDriver) Stop(ctx context.Context) (err error) {
	rd.Lock()
	defer rd.Unlock()
	rd.runtimeCancel()
	rd.started = false
	return
}

func (rd *RedisDriver) GetNodes(ctx context.Context) (nodes []string, err error) {
	mathStr := fmt.Sprintf("%s*", commons.GetKeyPre(rd.serviceName))
	return rd.scan(ctx, mathStr)
}

// private function

func (rd *RedisDriver) heartBeat() {
	tick := time.NewTicker(rd.timeout / 2)
	for {
		select {
		case <-tick.C:
			{
				if err := rd.registerServiceNode(); err != nil {
					rd.logger.Errorf("register service node error %+v", err)
				}
			}
		case <-rd.runtimeCtx.Done():
			{
				if err := rd.c.Del(context.Background(), rd.nodeID, rd.nodeID).Err(); err != nil {
					rd.logger.Errorf("unregister service node error %+v", err)
				}
				return
			}
		}
	}
}

func (rd *RedisDriver) registerServiceNode() error {
	return rd.c.SetEx(context.Background(), rd.nodeID, rd.nodeID, rd.timeout).Err()
}

func (rd *RedisDriver) scan(ctx context.Context, matchStr string) ([]string, error) {
	ret := make([]string, 0)
	iter := rd.c.Scan(ctx, 0, matchStr, -1).Iterator()
	for iter.Next(ctx) {
		err := iter.Err()
		if err != nil {
			return nil, err
		}
		ret = append(ret, iter.Val())
	}
	return ret, nil
}

func (rd *RedisDriver) WithOption(opt commons.Option) (err error) {
	switch opt.Type() {
	case commons.OptionTypeTimeout:
		{
			rd.timeout = opt.(commons.TimeoutOption).Timeout
		}
	case commons.OptionTypeLogger:
		{
			rd.logger = opt.(commons.LoggerOption).Logger
		}
	}
	return
}
