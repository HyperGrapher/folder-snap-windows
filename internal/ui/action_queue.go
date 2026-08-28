package ui

type actionQueue struct{ pending chan func() }

func newActionQueue(capacity int) *actionQueue {
	return &actionQueue{pending: make(chan func(), capacity)}
}

func (queue *actionQueue) enqueue(action, wake func()) {
	queue.pending <- action
	if wake != nil {
		wake()
	}
}

func (queue *actionQueue) drain() {
	for {
		select {
		case action := <-queue.pending:
			action()
		default:
			return
		}
	}
}
