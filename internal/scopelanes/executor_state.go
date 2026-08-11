package scopelanes

import (
	"context"
	"sync"
)

type Executor struct {
	config Config

	mu          sync.Mutex
	started     bool
	closed      bool
	lanes       map[string]*scopeLane
	laneChanged chan struct{}
	semaphore   chan struct{}
	laneWG      sync.WaitGroup
}

type scopeLane struct {
	pending []scopeLaneWork
	running bool
}

type scopeLaneWork struct {
	ctx  context.Context
	run  func(context.Context) error
	done chan error
}

func NewExecutor(config Config) (*Executor, error) {
	normalized, err := config.normalized()
	if err != nil {
		return nil, err
	}
	return &Executor{
		config:      normalized,
		lanes:       make(map[string]*scopeLane),
		laneChanged: make(chan struct{}),
		semaphore:   make(chan struct{}, normalized.MaxConcurrentScopes),
	}, nil
}
