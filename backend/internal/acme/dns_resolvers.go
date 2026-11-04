package acme

import (
	"context"
	"slices"
	"sync"

	"github.com/go-acme/lego/v5/challenge/dns01"
)

var dnsResolvers resolverLeases

// Lego v5 的 DNS provider、CNAME 查询和传播检查共用全局 DNS client。
// 相同配置的签发共享租约，保持并发；切换配置必须等旧配置的签发全部结束。
type resolverLeases struct {
	mu         sync.Mutex
	resolvers  []string
	configured bool
	active     int
	idle       chan struct{}
}

func (l *resolverLeases) acquire(ctx context.Context, resolvers []string) (func(), error) {
	for {
		l.mu.Lock()
		if err := ctx.Err(); err != nil {
			l.mu.Unlock()
			return nil, err
		}
		if l.active == 0 || slices.Equal(l.resolvers, resolvers) {
			if l.active == 0 {
				if !l.configured || !slices.Equal(l.resolvers, resolvers) {
					opts := dns01.NewOptions()
					opts.RecursiveNameservers = slices.Clone(resolvers)
					dns01.SetDefaultClient(dns01.NewClient(opts))
					l.resolvers = slices.Clone(resolvers)
					l.configured = true
				}
				l.idle = make(chan struct{})
			}
			l.active++
			l.mu.Unlock()
			return l.release, nil
		}
		idle := l.idle
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-idle:
		}
	}
}

func (l *resolverLeases) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active--
	if l.active == 0 {
		close(l.idle)
	}
}
