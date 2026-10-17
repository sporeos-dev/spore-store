// Copyright 2026 Matt Harrison
// SPDX-License-Identifier: Apache-2.0

package await

import (
	"errors"
	"sync"
	"time"

	"github.com/sporeos-dev/spore-client-libs/spore_go/response"
)

type result struct {
	res *response.Response
	err *response.ResponseError
}

type Pending struct {
	mu       sync.Mutex
	channels map[string]chan result
	timeout  time.Duration
}

func New() *Pending {
	return &Pending{
		channels: make(map[string]chan result),
		timeout:  5 * time.Second,
	}
}

func (p *Pending) WithTimeout(timeout time.Duration) *Pending {
	p.timeout = timeout
	return p
}

func (p *Pending) Await(handle string) {
	ch := make(chan result, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.channels[handle] = ch
}

func (p *Pending) WaitFor(handle string) (*response.Response, *response.ResponseError, error) {
	p.mu.Lock()
	ch, ok := p.channels[handle]
	p.mu.Unlock()
	if !ok {
		return nil, nil, errors.New("Missing")
	}

	select {
	case res := <-ch:
		return res.res, res.err, nil
	case <-time.After(p.timeout):
		p.mu.Lock()
		delete(p.channels, handle)
		p.mu.Unlock()
		return nil, nil, errors.New("Timeout")
	}
}

func (p *Pending) Receive(res *response.Response, err *response.ResponseError) {
	var handle string
	if res != nil {
		handle = res.Handle()
	} else if err != nil {
		handle = err.Handle()
	} else {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	ch, ok := p.channels[handle]
	if ok {
		delete(p.channels, handle)
		ch <- result{
			res: res,
			err: err,
		}
	}
}
