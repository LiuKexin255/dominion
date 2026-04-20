package domain

import (
	"context"
	"sync"
)

const (
	maxQueueCap = 256
)

type enqueueSource int

const (
	enqueueSourceRetry enqueueSource = iota
	enqueueSourceUser
)

// WorkItem is a single in-memory queue item for an environment.
type WorkItem struct {
	EnvName    EnvironmentName
	RetryCount int
	Source     enqueueSource
}

// Queue is an in-memory single-lane queue for environment operations.
type Queue struct {
	mu sync.Mutex

	items     map[EnvironmentName]*WorkItem
	followUps map[EnvironmentName]*WorkItem
	inFlight  map[EnvironmentName]bool

	pendingCh chan EnvironmentName
	done      chan struct{}
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

// Start initializes the queue lifecycle. It must be called before Dequeue.
// Items enqueued before Start are buffered and will be available for Dequeue.
func (q *Queue) Start(_ context.Context) error {
	return nil
}

// Stop signals the queue to shut down. Any goroutine blocked on Dequeue will
// receive zero value and false.
func (q *Queue) Stop() {
	close(q.done)
}

// Enqueue adds a user work item for envName.
func (q *Queue) Enqueue(_ context.Context, envName EnvironmentName) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	item := &WorkItem{EnvName: envName, RetryCount: 0, Source: enqueueSourceUser}

	if _, ok := q.items[envName]; ok {
		q.items[envName] = item
		return nil
	}

	if q.inFlight[envName] {
		q.followUps[envName] = item
		return nil
	}

	q.enqueueLocked(item)
	return nil
}

// EnqueueRetry adds a retry work item.
func (q *Queue) EnqueueRetry(_ context.Context, item *WorkItem) error {
	envName := item.EnvName
	item.Source = enqueueSourceRetry

	q.mu.Lock()
	defer q.mu.Unlock()

	if queuedItem, ok := q.items[envName]; ok {
		if queuedItem.Source == enqueueSourceUser {
			return nil
		}

		q.items[envName] = item
		return nil
	}

	if q.inFlight[envName] {
		if followUpItem, ok := q.followUps[envName]; ok && followUpItem.Source == enqueueSourceUser {
			return nil
		}

		q.followUps[envName] = item
		return nil
	}

	q.enqueueLocked(item)
	return nil
}

// Dequeue retrieves the next work item.
// It blocks until an item is available, the context is cancelled, or Stop is called.
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

	q.enqueueLocked(followUpItem)
}

func (q *Queue) enqueueLocked(item *WorkItem) {
	envName := item.EnvName
	q.items[envName] = item
	q.pendingCh <- envName
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
