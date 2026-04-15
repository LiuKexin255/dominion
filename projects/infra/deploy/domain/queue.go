package domain

import (
	"context"
	"sync"
)

const (
	maxQueueCap = 256
)

// queueItem represents an item in the queue with a priority flag.
type queueItem struct {
	envName  EnvironmentName
	priority bool
}

// Queue is an in-memory priority queue for environment operations.
//
// It guarantees:
//   - Priority items (delete) are dequeued before normal items (reconcile).
//   - The same EnvironmentName is not duplicated in the queue; enqueuing an
//     already-queued envName is a no-op (idempotent). If the envName is already
//     present as a normal item and EnqueueWithPriority is called, it is upgraded
//     to priority. Normal enqueue of an already-priority item is a no-op.
//   - After an envName is dequeued, it can be re-enqueued.
type Queue struct {
	mu sync.Mutex

	// pending tracks envNames currently waiting in the queue.
	// Value is true if the item is in the priority lane.
	pending map[EnvironmentName]bool

	// normalCh receives normal-priority items.
	normalCh chan EnvironmentName
	// priorityCh receives priority items.
	priorityCh chan EnvironmentName

	// done is closed when Stop is called.
	done chan struct{}
}

// NewQueue creates a new Queue.
func NewQueue() *Queue {
	return &Queue{
		pending:    map[EnvironmentName]bool{},
		normalCh:   make(chan EnvironmentName, maxQueueCap),
		priorityCh: make(chan EnvironmentName, maxQueueCap),
		done:       make(chan struct{}),
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

// Enqueue adds an envName to the normal-priority queue. If the envName is
// already pending, this is a no-op. If it is already pending as a priority
// item, the priority is preserved.
func (q *Queue) Enqueue(_ context.Context, envName EnvironmentName) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	_, exists := q.pending[envName]
	if exists {
		// Already pending; if already priority, keep it. Normal stays normal.
		return nil
	}

	q.pending[envName] = false
	q.normalCh <- envName
	return nil
}

// EnqueueWithPriority adds an envName to the priority queue. If the envName
// is already pending as a normal item, it is upgraded to priority.
func (q *Queue) EnqueueWithPriority(_ context.Context, envName EnvironmentName) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	isPriority, exists := q.pending[envName]
	if exists {
		if !isPriority {
			// Upgrade: move from normal to priority.
			// Drain the normal channel for this item.
			q.upgradeToPriority(envName)
			q.pending[envName] = true
		}
		return nil
	}

	q.pending[envName] = true
	q.priorityCh <- envName
	return nil
}

// Dequeue retrieves the next envName. Priority items are served first.
// It blocks until an item is available, the context is cancelled, or Stop is called.
func (q *Queue) Dequeue(ctx context.Context) (EnvironmentName, bool) {
	// Fast-path: try priority channel first (non-blocking).
	select {
	case envName := <-q.priorityCh:
		q.removeFromPending(envName)
		return envName, true
	default:
	}

	// Slow-path: wait for any item or cancellation.
	for {
		select {
		case <-ctx.Done():
			return EnvironmentName{}, false
		case <-q.done:
			return EnvironmentName{}, false
		case envName := <-q.priorityCh:
			q.removeFromPending(envName)
			return envName, true
		case envName := <-q.normalCh:
			// Check if this was upgraded to priority while waiting.
			q.mu.Lock()
			isPriority := q.pending[envName]
			q.mu.Unlock()
			if isPriority {
				// This item was upgraded; skip it and wait for the priority version.
				continue
			}
			q.removeFromPending(envName)
			return envName, true
		}
	}
}

// upgradeToPriority drains a specific envName from the normal channel.
// Caller must hold q.mu.
func (q *Queue) upgradeToPriority(target EnvironmentName) {
	// Drain normalCh into a temp buffer, skipping the target, then put back.
	var buf []EnvironmentName
	for {
		select {
		case item := <-q.normalCh:
			if item == target {
				// Found and removed; put the rest back.
				for _, remaining := range buf {
					q.normalCh <- remaining
				}
				// Now send to priority channel.
				q.priorityCh <- target
				return
			}
			buf = append(buf, item)
		default:
			// Target not found in channel buffer; it may have been consumed
			// already. Send to priority channel and let Dequeue handle it.
			q.priorityCh <- target
			for _, remaining := range buf {
				q.normalCh <- remaining
			}
			return
		}
	}
}

// removeFromPending removes the envName from the pending set, allowing it
// to be re-enqueued.
func (q *Queue) removeFromPending(envName EnvironmentName) {
	q.mu.Lock()
	delete(q.pending, envName)
	q.mu.Unlock()
}
