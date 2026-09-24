// Package comms is gopyter's implementation of GoNB's gonbui/comms
// package: values exchanged with the front-end, by address. Widgets use it.
//
// In gopyter, when the user presses Done (or there is no UI to send
// values, as in gopyter run), every channel from Listen is closed, so
// loops over them end.
//
// Its API and documentation are adapted from GoNB's gonbui/comms package
// (https://github.com/janpfeifer/gonb/tree/main/gonbui/comms), by Jan
// Pfeifer, under the MIT license: see LICENSE in the enclosing gonb
// directory.
package comms

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"

	"github.com/mark3labs/gopyter/internal/kernel/runtime/gonb/gonbui"
	"github.com/mark3labs/gopyter/internal/kernel/runtime/gonb/gonbui/protocol"
	"github.com/mark3labs/gopyter/internal/kernel/runtime/gopyter/nb/wire"
)

// Start is kept for compatibility: gopyter needs no setup.
func Start() {}

// Send sends value to the front-end at address, e.g. to set a widget.
func Send[T protocol.CommValueTypes](address string, value T) {
	if gonbui.IsNotebook {
		wire.Op(map[string]any{"op": "value", "address": address, "value": value})
	}
}

// ReadValue asks the front-end for the value at address. It returns the
// zero value if there is none, or outside a notebook.
func ReadValue[T protocol.CommValueTypes](address string) (value T) {
	raw, ok := wire.Request(map[string]any{"op": "read", "address": address})
	if !ok {
		return value
	}
	value, _ = decode[T](raw) // a value of another type reads as zero
	return value
}

// SubscriptionId identifies a subscription, for Unsubscribe.
type SubscriptionId int

var (
	subMu   sync.Mutex
	cancels = map[SubscriptionId]func(){}
	nextID  SubscriptionId
)

// Subscribe calls callback with every value the front-end sends to address.
func Subscribe[T protocol.CommValueTypes](address string, callback func(address string, value T)) SubscriptionId {
	cancel := wire.Subscribe(address, func(raw json.RawMessage) {
		v, err := decode[T](raw)
		if err != nil {
			log.Printf("Warning: gonbui/comms: value for address %q: %v", address, err)
		}
		callback(address, v)
	})
	subMu.Lock()
	defer subMu.Unlock()
	id := nextID
	nextID++
	cancels[id] = cancel
	return id
}

// Unsubscribe ends a subscription.
func Unsubscribe(id SubscriptionId) {
	subMu.Lock()
	cancel := cancels[id]
	delete(cancels, id)
	subMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func decode[T protocol.CommValueTypes](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 || string(raw) == "null" {
		return v, nil
	}
	if json.Unmarshal(raw, &v) == nil {
		return v, nil
	}
	var a any
	if err := json.Unmarshal(raw, &a); err != nil {
		return v, err
	}
	return ConvertTo[T](a)
}

// ConvertTo converts a value received from the front-end to T.
func ConvertTo[T protocol.CommValueTypes](from any) (to T, err error) {
	if v, ok := from.(T); ok {
		return v, nil
	}
	var target any = to
	switch target.(type) {
	case int:
		switch f := from.(type) {
		case float64:
			target = int(math.Round(f))
		case float32:
			target = int(math.Round(float64(f)))
		case string:
			n, err := strconv.Atoi(f)
			if err != nil {
				return to, fmt.Errorf("failed to convert %q to int: %w", f, err)
			}
			target = n
		default:
			return to, fmt.Errorf("failed to convert type %T (%v) to int", from, from)
		}
		return target.(T), nil
	case float64:
		switch f := from.(type) {
		case int:
			target = float64(f)
		case float32:
			target = float64(f)
		case string:
			n, err := strconv.ParseFloat(f, 64)
			if err != nil {
				return to, fmt.Errorf("failed to convert %q to float64: %w", f, err)
			}
			target = n
		default:
			return to, fmt.Errorf("failed to convert type %T (%v) to float64", from, from)
		}
		return target.(T), nil
	}
	// Slices and maps: through JSON.
	b, err := json.Marshal(from)
	if err == nil {
		err = json.Unmarshal(b, &to)
	}
	if err != nil {
		return to, fmt.Errorf("failed to convert type %T (%v) to %T: %w", from, from, to, err)
	}
	return to, nil
}

// AddressChan is a channel of the values sent to an address. Receive them
// from C.
type AddressChan[T protocol.CommValueTypes] struct {
	C chan T

	mu         sync.Mutex
	closed     bool
	closedC    chan struct{}
	latestOnly bool
	sub        SubscriptionId
}

// open channels, closed when the user is done.
var (
	openMu sync.Mutex
	open   = map[any]func(){}
)

func init() {
	go func() {
		<-wire.Done()
		openMu.Lock()
		closers := make([]func(), 0, len(open))
		for _, c := range open {
			closers = append(closers, c)
		}
		openMu.Unlock()
		for _, c := range closers {
			c()
		}
	}()
}

// Listen returns a channel receiving the values sent to address. Close it
// when done.
func Listen[T protocol.CommValueTypes](address string) *AddressChan[T] {
	c := &AddressChan[T]{C: make(chan T), closedC: make(chan struct{})}
	c.sub = Subscribe(address, func(_ string, v T) { c.send(v) })
	openMu.Lock()
	open[c] = c.Close
	openMu.Unlock()
	select {
	case <-wire.Done():
		c.Close() // the user was already done
	default:
	}
	return c
}

func (c *AddressChan[T]) send(v T) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	ch, latest := c.C, c.latestOnly
	c.mu.Unlock()
	defer func() {
		_ = recover() // closed while sending
	}()
	if latest {
		for {
			select {
			case ch <- v:
				return
			case <-c.closedC:
				return
			case <-ch: // drop the stale value
			}
		}
	}
	select {
	case ch <- v:
	case <-c.closedC:
	}
}

// WithBuffer gives C a buffer of n values. Call it right after Listen.
func (c *AddressChan[T]) WithBuffer(n int) *AddressChan[T] {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.C = make(chan T, n)
	}
	return c
}

// LatestOnly makes C keep only the latest value: older unread values are
// dropped. Call it right after Listen.
func (c *AddressChan[T]) LatestOnly() *AddressChan[T] {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.C = make(chan T, 1)
		c.latestOnly = true
	}
	return c
}

// IsClosed reports whether the channel was closed.
func (c *AddressChan[T]) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Close closes C and stops listening.
func (c *AddressChan[T]) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.closedC)
	close(c.C)
	c.mu.Unlock()
	Unsubscribe(c.sub)
	openMu.Lock()
	delete(open, c)
	openMu.Unlock()
}

// WaitClose waits until the channel is closed.
func (c *AddressChan[T]) WaitClose() { <-c.closedC }
