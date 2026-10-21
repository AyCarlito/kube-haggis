package kube

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	relatedRequestDeadline  = 2 * time.Second
	relatedCallBudgetLimit  = 32
	relatedConcurrencyLimit = 8
)

var errRelatedCallBudget = errors.New("relationship Kubernetes call budget exhausted")

// relatedCallBudget bounds relationship API calls and their concurrent execution.
type relatedCallBudget struct {
	ctx         context.Context
	limit       int
	concurrency chan struct{}
	mu          sync.Mutex
	calls       int
}

// newRelatedCallBudget creates the bounded call accounting used by one request.
func newRelatedCallBudget(ctx context.Context) *relatedCallBudget {
	return &relatedCallBudget{
		ctx:         ctx,
		limit:       relatedCallBudgetLimit,
		concurrency: make(chan struct{}, relatedConcurrencyLimit),
	}
}

// call runs one Kubernetes operation when the request still has safe capacity.
func (budget *relatedCallBudget) call(operation func() error) error {
	if err := budget.ctx.Err(); err != nil {
		return err
	}
	select {
	case budget.concurrency <- struct{}{}:
	case <-budget.ctx.Done():
		return budget.ctx.Err()
	}
	defer func() { <-budget.concurrency }()

	budget.mu.Lock()
	if budget.calls >= budget.limit {
		budget.mu.Unlock()
		return errRelatedCallBudget
	}
	budget.calls++
	budget.mu.Unlock()
	return operation()
}

// count returns the number of Kubernetes operations admitted by the budget.
func (budget *relatedCallBudget) count() int {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.calls
}

// RelatedDiagnostics contains safe relationship query counts and timing data.
type RelatedDiagnostics struct {
	QueryCount           int   `json:"queryCount"`
	QueryBudget          int   `json:"queryBudget"`
	DurationMilliseconds int64 `json:"durationMilliseconds"`
	DeadlineMilliseconds int64 `json:"deadlineMilliseconds"`
	ConcurrentLimit      int   `json:"concurrentLimit"`
}

// relatedDiagnostics builds non-sensitive diagnostics for one relationship operation.
func relatedDiagnostics(budget *relatedCallBudget, started time.Time, deadline time.Duration) RelatedDiagnostics {
	return RelatedDiagnostics{
		QueryCount:           budget.count(),
		QueryBudget:          budget.limit,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		DeadlineMilliseconds: deadline.Milliseconds(),
		ConcurrentLimit:      relatedConcurrencyLimit,
	}
}
