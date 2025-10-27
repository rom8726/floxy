package rate_limiter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rom8726/floxy"
)

var _ floxy.Plugin = (*RateLimiterPlugin)(nil)

type RateLimiterPlugin struct {
	floxy.BasePlugin

	tokens     map[string]int
	maxTokens  int
	refillRate time.Duration
	mu         sync.Mutex
	stopChan   chan struct{}
}

func New(maxTokens int, refillRate time.Duration) *RateLimiterPlugin {
	return &RateLimiterPlugin{
		BasePlugin: floxy.NewBasePlugin("rate_limiter", floxy.PriorityHigh),
		tokens:     make(map[string]int),
		maxTokens:  maxTokens,
		refillRate: refillRate,
		stopChan:   make(chan struct{}),
	}
}

func (p *RateLimiterPlugin) Initialize() error {
	// Start token refill goroutine
	go p.refillTokens()

	return nil
}

func (p *RateLimiterPlugin) OnStepStart(
	_ context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := fmt.Sprintf("%s:%s", instance.WorkflowID, step.StepName)

	if _, exists := p.tokens[key]; !exists {
		p.tokens[key] = p.maxTokens
	}

	if p.tokens[key] <= 0 {
		return fmt.Errorf("rate limit exceeded for %s", key)
	}

	p.tokens[key]--

	return nil
}

func (p *RateLimiterPlugin) Shutdown() error {
	close(p.stopChan)

	return nil
}

func (p *RateLimiterPlugin) refillTokens() {
	ticker := time.NewTicker(p.refillRate)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.mu.Lock()
			for key := range p.tokens {
				if p.tokens[key] < p.maxTokens {
					p.tokens[key]++
				}
			}
			p.mu.Unlock()
		}
	}
}
