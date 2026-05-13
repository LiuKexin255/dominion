package domain

import (
	"context"
	"sync"
	"time"
)

const (
	maxQueueCap = 256
)

// WorkItemSource identifies the origin of a work item in the queue.
type WorkItemSource int

const (
	// WorkItemSourceRetry is a retry from a previous failed attempt.
	WorkItemSourceRetry WorkItemSource = iota
	// WorkItemSourceUser is a user-triggered enqueue.
	WorkItemSourceUser
	// WorkItemSourcePoll is a poll-triggered enqueue.
	WorkItemSourcePoll
)

// WorkItem is a single in-memory queue item for an environment.
type WorkItem struct {
	EnvName    EnvironmentName
	RetryCount int
	Source     WorkItemSource
}

// Queue is an in-memory single-lane queue for environment operations.
type Queue struct {
	mu sync.Mutex

	items     map[EnvironmentName]*WorkItem
	followUps map[EnvironmentName]*WorkItem
	inFlight  map[EnvironmentName]bool

	pendingCh chan EnvironmentName
	done      chan struct{}
	stopOnce  sync.Once
}

// NewQueue creates a new Queue.
func NewQueue() *Queue {
	return &Queue{
		items:     map[EnvironmentName]*WorkItem{},
		followUps: map[EnvironmentName]*WorkItem{},
		inFlight:  map[EnvironmentName]bool{},
		pendingCh: make(chan EnvironmentName, maxQueueCap),
		done:      make(chan struct{}),
	}
}

// stop signals the queue to shut down. Any goroutine blocked on Dequeue will
// receive zero value and false. It is idempotent.
func (q *Queue) stop() {
	q.stopOnce.Do(func() { close(q.done) })
}

// Enqueue adds a user work item for envName using WorkItemSourceUser. It resets the retry count to zero.
func (q *Queue) Enqueue(_ context.Context, envName EnvironmentName) error {
	q.enqueueNow(&WorkItem{EnvName: envName, RetryCount: 0, Source: WorkItemSourceUser})
	return nil
}

// EnqueueAfter schedules the item to be enqueued after the given delay.
// The item's Source field determines dedup behavior: user overrides everything,
// retry overrides retry/poll, poll only overrides poll.
// If delay is zero or negative, the item is enqueued immediately.
// If ctx is cancelled before enqueue, the item is dropped.
func (q *Queue) EnqueueAfter(ctx context.Context, item *WorkItem, delay time.Duration) error {
	if delay <= 0 {
		q.enqueueNow(item)
		return nil
	}

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		if ctx.Err() != nil {
			return
		}

		q.enqueueNow(item)
	}()

	return nil
}

// Dequeue retrieves the next work item.
// It blocks until an item is available, the context is cancelled, or stop is called.
func (q *Queue) Dequeue(ctx context.Context) (*WorkItem, bool) {
	for {
		select {
		case <-ctx.Done():
			return nil, false
		case <-q.done:
			return nil, false
		case envName := <-q.pendingCh:
			item, ok := q.markInFlight(envName)
			if !ok {
				continue
			}
			return item, true
		}
	}
}

// Complete marks the current in-flight item as finished and schedules any follow-up item.
func (q *Queue) Complete(envName EnvironmentName) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.inFlight, envName)

	followUpItem, ok := q.followUps[envName]
	if !ok {
		return
	}
	delete(q.followUps, envName)

	if _, ok := q.items[envName]; ok || q.inFlight[envName] {
		return
	}

	q.items[envName] = followUpItem
	go func() { q.pendingCh <- envName }()
}

// enqueueNow enqueues the item immediately, respecting source-priority dedup rules.
func (q *Queue) enqueueNow(item *WorkItem) {
	envName := item.EnvName

	q.mu.Lock()
	defer q.mu.Unlock()

	if queuedItem, ok := q.items[envName]; ok {
		if !canOverride(queuedItem, item) {
			return
		}
		q.items[envName] = item
		return
	}
	if q.inFlight[envName] {
		if followUpItem, ok := q.followUps[envName]; ok && !canOverride(followUpItem, item) {
			return
		}
		q.followUps[envName] = item
		return
	}
	q.items[envName] = item
	go func() { q.pendingCh <- envName }()
}

// canOverride returns true when incoming may replace existing based on source priority.
// User overrides everything; retry overrides retry and poll, not user; poll only overrides poll.
func canOverride(existing, incoming *WorkItem) bool {
	switch incoming.Source {
	case WorkItemSourceUser:
		return true
	case WorkItemSourceRetry:
		return existing.Source != WorkItemSourceUser
	case WorkItemSourcePoll:
		return existing.Source == WorkItemSourcePoll
	default:
		return false
	}
}

func (q *Queue) markInFlight(envName EnvironmentName) (*WorkItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, ok := q.items[envName]
	if !ok {
		return nil, false
	}

	delete(q.items, envName)
	q.inFlight[envName] = true

	return item, true
}
