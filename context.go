package main

import (
	"context"
	"sync"
)

// AppContext represents the application-wide context structure
type AppContext struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// NewAppContext creates a new application context
func NewAppContext() *AppContext {
	ctx, cancel := context.WithCancel(context.Background())
	return &AppContext{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Context returns the current context
func (a *AppContext) Context() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

// Cancel cancels the current context
func (a *AppContext) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// Reset resets the context
func (a *AppContext) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ctx, a.cancel = context.WithCancel(context.Background())
}

// GlobalAppContext is the application-wide context instance
var GlobalAppContext = NewAppContext()
