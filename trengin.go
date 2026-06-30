// Package trengin provides a framework for creating a trading bot.
// It defines the Strategy and Broker interfaces, allowing their
// implementations to be connected through an Engine instance.
//
// Strategy can flexibly perform actions to open a new position
// (OpenPositionAction), change a position's conditional order (stop-loss and take-profit)
// (ChangeConditionalOrderAction), and close a position (ClosePositionAction).
//
// Broker must implement functionality for opening a trade, tracking conditional order status,
// changing conditional orders, and closing positions.
//
// Additional functionality can be implemented by setting callbacks
// for position change events using OnPositionOpened, OnPositionClosed,
// and OnConditionalOrderChanged.
package trengin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

var (
	ErrSendResultTimeout = errors.New("send result timeout")
	ErrUnknownAction     = errors.New("unknown action")
	ErrAlreadyClosed     = errors.New("already closed")
	ErrActionNotValid    = errors.New("action not valid")
)

type (
	PositionID   uuid.UUID
	PositionType int
)

const (
	Long PositionType = iota + 1
	Short
)

// Multiplier returns 1 for Long, -1 for Short,
// and 0 for any other value. It can be used as a multiplier
// in calculations that depend on position type, for example,
// when calculating position profit.
func (t PositionType) Multiplier() float64 {
	switch t {
	case Long:
		return 1
	case Short:
		return -1
	default:
		return 0
	}
}

// IsLong returns true if position is long
func (t PositionType) IsLong() bool {
	return t == Long
}

// IsShort returns true if position is short
func (t PositionType) IsShort() bool {
	return t == Short
}

// IsValid returns true if position is valid
func (t PositionType) IsValid() bool {
	return t == Long || t == Short
}

// Inverse returns inverted position type
func (t PositionType) Inverse() PositionType {
	if t.IsShort() {
		return Long
	}
	return Short
}

// NewPositionID creates unique position ID
func NewPositionID() PositionID {
	return PositionID(uuid.New())
}

func (p PositionID) String() string {
	return uuid.UUID(p).String()
}

//go:generate docker run --rm -v ${PWD}:/app -w /app/ vektra/mockery --name Strategy --inpackage --case snake

// Strategy describes the trading strategy interface. It allows implementing a strategy
// by interacting with Engine through the channel returned by the Actions method.
// Actions is used to send trading actions. If the Actions channel is closed,
// Engine will terminate.
type Strategy interface {
	// Run starts the strategy.
	Run(ctx context.Context, actions Actions) error
}

// Actions is a channel for passing trading actions from Strategy to Broker.
// It can accept OpenPositionAction, ClosePositionAction, and ChangeConditionalOrderAction.
// Unexpected types will cause an error and terminate the Engine.
type Actions chan interface{}

//go:generate docker run --rm -v ${PWD}:/app -w /app/ vektra/mockery --name BrokerRunner --inpackage --case snake
type BrokerRunner interface {
	Broker
	Runner
}

//go:generate docker run --rm -v ${PWD}:/app -w /app/ vektra/mockery --name Broker --inpackage --case snake

// Broker describes client for execution of trading operations.
type Broker interface {
	// OpenPosition opens a position and returns Position and PositionClosed channel,
	// which will be sent closed position.
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)

	// ClosePosition closes a position and returns closed position.
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)

	// ChangeConditionalOrder changes conditional orders and returns changed position.
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}

// Runner can be implemented Broker client to starts background tasks
// such as tracking open position.
type Runner interface {
	Run(ctx context.Context) error
}

// PositionClosed is a channel that receives a position when it is closed.
type PositionClosed <-chan Position

// Position is a trading position. The ID is unique only within a single run.
type Position struct {
	ID            PositionID
	SecurityBoard string // Trading mode identifier. Example, TQBR
	SecurityCode  string // Example, SBER
	FIGI          string // Financial Instrument Global Identifier
	Type          PositionType
	Quantity      int64
	OpenTime      time.Time
	OpenPrice     float64
	CloseTime     time.Time
	ClosePrice    float64
	StopLoss      float64
	TakeProfit    float64
	Commission    float64

	extraMtx   *sync.RWMutex
	extra      map[interface{}]interface{}
	closedOnce *sync.Once
	closed     chan struct{}
}

// NewPosition creates a new position from action with open time openTime
// and open price openPrice. Returns ErrActionNotValid if action is invalid.
func NewPosition(action OpenPositionAction, openTime time.Time, openPrice float64) (*Position, error) {
	if !action.IsValid() {
		return nil, ErrActionNotValid
	}
	var stopLoss, takeProfit float64
	if action.StopLossOffset != 0 {
		stopLoss = openPrice - action.StopLossOffset*action.Type.Multiplier()
	}
	if action.TakeProfitOffset != 0 {
		takeProfit = openPrice + action.TakeProfitOffset*action.Type.Multiplier()
	}
	return &Position{
		ID:            NewPositionID(),
		SecurityBoard: action.SecurityBoard,
		SecurityCode:  action.SecurityCode,
		FIGI:          action.FIGI,
		Type:          action.Type,
		Quantity:      action.Quantity,
		OpenTime:      openTime,
		OpenPrice:     openPrice,
		StopLoss:      stopLoss,
		TakeProfit:    takeProfit,
		extraMtx:      &sync.RWMutex{},
		extra:         make(map[interface{}]interface{}),
		closed:        make(chan struct{}),
		closedOnce:    &sync.Once{},
	}, nil
}

// Close closes the position with close time closeTime and close price closePrice.
// A repeated call returns ErrAlreadyClosed; in that case the close time and price
// will not change.
func (p *Position) Close(closeTime time.Time, closePrice float64) (err error) {
	err = ErrAlreadyClosed
	p.closedOnce.Do(func() {
		p.CloseTime = closeTime
		p.ClosePrice = closePrice
		close(p.closed)
		err = nil
	})
	return
}

// Closed returns a channel that will be closed when the position is closed.
func (p *Position) Closed() <-chan struct{} {
	return p.closed
}

// IsClosed returns true if position is closed
func (p *Position) IsClosed() bool {
	select {
	case <-p.Closed():
		return true
	default:
	}
	return false
}

// IsLong returns true if position is long
func (p *Position) IsLong() bool {
	return p.Type == Long
}

// IsShort returns true if position is short
func (p *Position) IsShort() bool {
	return p.Type == Short
}

// AddCommission add commission to position
func (p *Position) AddCommission(val float64) {
	p.Commission += val
}

// Profit returns the profit of a closed trade. To get unrealized profit
// for an open position, use the ProfitByPrice method.
func (p *Position) Profit() float64 {
	return p.UnitProfit() * float64(p.Quantity)
}

// UnitProfit returns profit per volume unit
func (p *Position) UnitProfit() float64 {
	return (p.ClosePrice-p.OpenPrice)*p.Type.Multiplier() - p.UnitCommission()
}

// UnitCommission returns commission per volume unit
func (p *Position) UnitCommission() float64 {
	return p.Commission / float64(p.Quantity)
}

// ProfitByPrice returns the position profit at the specified price.
func (p *Position) ProfitByPrice(price float64) float64 {
	return (price - p.OpenPrice) * p.Type.Multiplier() * float64(p.Quantity)
}

// Duration returns the duration of a closed trade.
func (p *Position) Duration() time.Duration {
	return p.CloseTime.Sub(p.OpenTime)
}

// Extra gets the value of an extra field by key.
// Returns nil if the value is not set.
func (p *Position) Extra(key interface{}) interface{} {
	p.extraMtx.RLock()
	defer p.extraMtx.RUnlock()
	return p.extra[key]
}

// SetExtra sets the value of an extra field with the given key.
// It can be used to store additional optional informational data
// when implementing a strategy or broker. Do not rely on this data
// when implementing Strategy or Broker logic.
// Exception: local use within a Strategy or Broker implementation.
func (p *Position) SetExtra(key interface{}, val interface{}) *Position {
	p.extraMtx.Lock()
	defer p.extraMtx.Unlock()
	p.extra[key] = val
	return p
}

// RangeExtra applies function f to all elements in the Extra list.
func (p *Position) RangeExtra(f func(key interface{}, val interface{})) {
	p.extraMtx.RLock()
	defer p.extraMtx.RUnlock()
	for k, v := range p.extra {
		f(k, v)
	}
}

// OpenPositionAction is an action to open a position
type OpenPositionAction struct {
	SecurityBoard    string // Trading mode identifier. Example, TQBR
	SecurityCode     string // Example, SBER
	FIGI             string // Financial Instrument Global Identifier
	Type             PositionType
	Quantity         int64
	StopLossOffset   float64 // Stop loss offset from the opening price. If 0 then stop loss is not set
	TakeProfitOffset float64 //  Take profit offset from the opening price. If 0 then stop loss is not set

	result chan OpenPositionActionResult
}

// IsValid checks whether the action is valid.
func (a *OpenPositionAction) IsValid() bool {
	return a.Type.IsValid() && a.Quantity > 0
}

// OpenPositionActionResult is the result of opening a position.
type OpenPositionActionResult struct {
	Position Position
	Closed   PositionClosed // Channel for tracking position closure.
	error    error
}

// NewOpenPositionAction creates OpenPositionAction with the given figi, type of position,
// quantity of lots, stop loss and take profit offsets. If offset is 0
// then conditional order is not set.
func NewOpenPositionAction(
	figi string,
	positionType PositionType,
	quantity int64,
	stopLossOffset float64,
	takeProfitOffset float64,
) OpenPositionAction {
	return OpenPositionAction{
		FIGI:             figi,
		Type:             positionType,
		Quantity:         quantity,
		StopLossOffset:   stopLossOffset,
		TakeProfitOffset: takeProfitOffset,
		result:           make(chan OpenPositionActionResult),
	}
}

// Result returns the result of the open position action.
func (a *OpenPositionAction) Result(ctx context.Context) (OpenPositionActionResult, error) {
	select {
	case <-ctx.Done():
		return OpenPositionActionResult{}, ctx.Err()
	case result := <-a.result:
		return result, result.error
	}
}

// ClosePositionAction describes an action to close a position.
type ClosePositionAction struct {
	PositionID PositionID
	result     chan ClosePositionActionResult
}

// NewClosePositionAction creates an action to close the position with positionID.
func NewClosePositionAction(positionID PositionID) ClosePositionAction {
	return ClosePositionAction{
		PositionID: positionID,
		result:     make(chan ClosePositionActionResult),
	}
}

// ClosePositionActionResult describes the result of closing a position.
type ClosePositionActionResult struct {
	Position Position
	error    error
}

// Result returns the result of the close position action.
func (a *ClosePositionAction) Result(ctx context.Context) (ClosePositionActionResult, error) {
	select {
	case <-ctx.Done():
		return ClosePositionActionResult{}, ctx.Err()
	case result := <-a.result:
		return result, result.error
	}
}

// ChangeConditionalOrderAction describes an action to change the conditional order
// of the position with PositionID. If StopLoss or TakeProfit is passed as 0,
// those values must not be changed.
type ChangeConditionalOrderAction struct {
	PositionID PositionID
	StopLoss   float64
	TakeProfit float64
	result     chan ChangeConditionalOrderActionResult
}

// Result returns the result of the change conditional order action.
func (a *ChangeConditionalOrderAction) Result(ctx context.Context) (ChangeConditionalOrderActionResult, error) {
	select {
	case <-ctx.Done():
		return ChangeConditionalOrderActionResult{}, ctx.Err()
	case result := <-a.result:
		return result, result.error
	}
}

// ChangeConditionalOrderActionResult describes the result of changing a conditional order.
type ChangeConditionalOrderActionResult struct {
	Position Position
	error    error
}

// NewChangeConditionalOrderAction creates an action to change the conditional order
// for the position with the given positionID and new stopLoss and takeProfit values.
// If stopLoss or takeProfit should not be changed, pass 0.
func NewChangeConditionalOrderAction(positionID PositionID, stopLoss, takeProfit float64) ChangeConditionalOrderAction {
	return ChangeConditionalOrderAction{
		PositionID: positionID,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		result:     make(chan ChangeConditionalOrderActionResult),
	}
}

type Option func(*Engine)

// WithPreventBrokerRun returns Option which sets preventBrokerRun.
// The default preventBrokerRun is false
func WithPreventBrokerRun(preventBrokerRun bool) Option {
	return func(t *Engine) {
		t.preventBrokerRun = preventBrokerRun
	}
}

// Engine is the trading engine. Create it using the New constructor.
type Engine struct {
	strategy                  Strategy
	broker                    Broker
	onPositionOpened          func(position Position)
	onPositionClosed          func(position Position)
	onConditionalOrderChanged func(position Position)
	sendResultTimeout         time.Duration
	preventBrokerRun          bool
}

// New creates an Engine instance and returns a pointer to it.
func New(strategy Strategy, broker Broker, opts ...Option) *Engine {
	engine := &Engine{
		strategy:          strategy,
		broker:            broker,
		sendResultTimeout: 1 * time.Second,
	}
	for _, opt := range opts {
		opt(engine)
	}
	return engine
}

// Run starts the strategy.
func (e *Engine) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	g, ctx := errgroup.WithContext(ctx)
	actions := make(Actions)

	runner, ok := e.broker.(Runner)
	if ok && !e.preventBrokerRun {
		g.Go(func() error {
			defer cancel()
			return runner.Run(ctx)
		})
	}

	g.Go(func() error {
		defer cancel()
		return e.strategy.Run(ctx, actions)
	})

	g.Go(func() error {
		defer cancel()
		return e.run(ctx, g, actions)
	})

	return g.Wait()
}

func (e *Engine) run(ctx context.Context, g *errgroup.Group, actions Actions) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case action, ok := <-actions:
			if !ok {
				return nil
			}
			switch action := action.(type) {
			case OpenPositionAction:
				if err := e.doOpenPosition(ctx, g, action); err != nil {
					return err
				}
			case ClosePositionAction:
				if err := e.doClosePosition(ctx, action); err != nil {
					return err
				}
			case ChangeConditionalOrderAction:
				if err := e.doChangeConditionalOrder(ctx, action); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%v: %w", action, ErrUnknownAction)
			}
		}
	}
}

// OnPositionOpened sets callback f for position opening.
// The current position is passed as a parameter to f.
// Returns a pointer to Engine, implementing a fluent interface.
//
// This method is not thread-safe. Do not call it from different goroutines
// or after starting the Engine.
func (e *Engine) OnPositionOpened(f func(position Position)) *Engine {
	e.onPositionOpened = f
	return e
}

// OnConditionalOrderChanged sets callback f for conditional order changes.
// The current position is passed as a parameter to f.
// Returns a pointer to Engine, implementing a fluent interface.
//
// This method is not thread-safe. Do not call it from different goroutines
// or after starting the Engine.
func (e *Engine) OnConditionalOrderChanged(f func(position Position)) *Engine {
	e.onConditionalOrderChanged = f
	return e
}

// OnPositionClosed sets callback f for position closing.
// The current position is passed as a parameter to f.
// Returns a pointer to Engine, implementing a fluent interface.
//
// This method is not thread-safe. Do not call it from different goroutines
// or after starting the Engine.
func (e *Engine) OnPositionClosed(f func(position Position)) *Engine {
	e.onPositionClosed = f
	return e
}

func (e *Engine) doOpenPosition(ctx context.Context, g *errgroup.Group, action OpenPositionAction) error {
	position, closed, err := e.broker.OpenPosition(ctx, action)
	closed1, closed2 := e.teePositionClosed(ctx.Done(), g, closed)
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(e.sendResultTimeout):
		return fmt.Errorf("open position: %w", ErrSendResultTimeout)
	case action.result <- OpenPositionActionResult{
		Position: position,
		Closed:   closed1,
		error:    err,
	}:
	}
	if err != nil {
		return nil
	}

	g.Go(func() error {
		select {
		case <-ctx.Done():
			return nil
		case position, ok := <-closed2:
			if !ok {
				return nil
			}
			if e.onPositionClosed != nil {
				e.onPositionClosed(position)
			}
			return nil
		}
	})

	if e.onPositionOpened != nil {
		e.onPositionOpened(position)
	}
	return nil
}

func (e *Engine) doClosePosition(ctx context.Context, action ClosePositionAction) error {
	position, err := e.broker.ClosePosition(ctx, action)

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(e.sendResultTimeout):
		return fmt.Errorf("close position: %w", ErrSendResultTimeout)
	case action.result <- ClosePositionActionResult{
		Position: position,
		error:    err,
	}:
	}
	return nil
}

func (e *Engine) doChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) error {
	position, err := e.broker.ChangeConditionalOrder(ctx, action)

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(e.sendResultTimeout):
		return fmt.Errorf("change conditional order: %w", ErrSendResultTimeout)
	case action.result <- ChangeConditionalOrderActionResult{
		Position: position,
		error:    err,
	}:
	}
	if err != nil {
		return nil
	}

	if e.onConditionalOrderChanged != nil {
		e.onConditionalOrderChanged(position)
	}
	return nil
}

func (e *Engine) teePositionClosed(
	done <-chan struct{},
	g *errgroup.Group,
	in PositionClosed,
) (PositionClosed, PositionClosed) {
	out1 := make(chan Position)
	out2 := make(chan Position)

	g.Go(func() error {
		defer close(out1)
		defer close(out2)
		for {
			select {
			case <-done:
				return nil
			case val, ok := <-in:
				if !ok {
					return nil
				}
				var out1, out2 = out1, out2
				for i := 0; i < 2; i++ {
					select {
					case <-done:
					case out1 <- val:
						out1 = nil
					case out2 <- val:
						out2 = nil
					}
				}
			}
		}
	})
	return out1, out2
}
