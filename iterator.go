package kivik

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

type iterator interface {
	// SetValue is called once, and should return a zero-value for the iterator
	// type.
	SetValue() interface{}
	Next(interface{}) error
	Close() error
}

// An Iterator allows ...
type Iterator struct {
	feed iterator

	mu      sync.RWMutex
	ready   bool // Set to true once Next() has been called
	closed  bool
	lasterr error // non-nil only if closed is true

	cancel func() // cancel function to exit context goroutine when iterator is closed

	curVal interface{}
}

func (i *Iterator) rlock() (unlock func(), err error) {
	i.mu.RLock()
	if i.closed {
		i.mu.RUnlock()
		return nil, errors.New("kivik: Iterator is closed")
	}
	if !i.ready {
		i.mu.RUnlock()
		return nil, errors.New("kivik: Iterator access before calling Next")
	}
	return func() { i.mu.RUnlock() }, nil
}

func newIterator(ctx context.Context, feed iterator) *Iterator {
	i := &Iterator{
		feed:   feed,
		curVal: feed.SetValue(),
	}
	ctx, i.cancel = context.WithCancel(ctx)
	go i.awaitDone(ctx)
	return i
}

// awaitDone blocks until the rows are closed or the context is cancelled, then closes the iterator if it's still open.
func (i *Iterator) awaitDone(ctx context.Context) {
	<-ctx.Done()
	_ = i.close(ctx.Err())
}

// Next prepares the next iterator result value for reading. It returns true on
// success, or false if there is no next result or an error occurs while
// preparing it. Err should be consulted to distinguish between the two.
func (i *Iterator) Next() bool {
	doClose, ok := i.next()
	if doClose {
		_ = i.Close()
	}
	return ok
}

func (i *Iterator) next() (doClose, ok bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.closed {
		return false, false
	}
	i.ready = true
	i.lasterr = i.feed.Next(i.curVal)
	if i.lasterr != nil {
		return true, false
	}
	return false, true
}

// Close closes the Iterator, preventing further enumeration, and freeing any
// resources (such as the http request body) of the underlying feed. If Next is
// called and there are no further results, Iterator is closed automatically and
// it will suffice to check the result of Err. Close is idempotent and does not
// affect the result of Err.
func (i *Iterator) Close() error {
	return i.close(nil)
}

func (i *Iterator) close(err error) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true

	if i.lasterr == nil {
		i.lasterr = err
	}

	err = i.feed.Close()

	if i.cancel != nil {
		i.cancel()
	}

	return err
}

// Err returns the error, if any, that was encountered during iteration. Err
// may be called after an explicit or implicit Close.
func (i *Iterator) Err() error {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.lasterr == io.EOF {
		return nil
	}
	return i.lasterr
}

func scan(dest interface{}, val json.RawMessage) error {
	switch d := dest.(type) {
	case *[]byte:
		if d == nil {
			return errNilPtr
		}
		tgt := make([]byte, len(val))
		copy(tgt, val)
		*d = tgt
		return nil
	case *json.RawMessage:
		if d == nil {
			return errNilPtr
		}
		*d = val
		return nil
	}
	return json.Unmarshal(val, dest)
}