package trengin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrSendResultTimeout = errors.New("send result timeout")
	ErrUnknownAction     = errors.New("unknown action")
	ErrAlreadyClosed     = errors.New("already closed")
	ErrActionNotValid    = errors.New("action not valid")
)

type (
	PositionID   int64
	PositionType int
)

const (
	LongPosition PositionType = iota + 1
	ShortPosition
)

func (t PositionType) Multiplier() float64 {
	if t == LongPosition {
		return 1
	}
	return -1
}

type Position struct {
	ID         PositionID
	Type       PositionType
	OpenTime   time.Time
	OpenPrice  float64
	CloseTime  time.Time
	ClosePrice float64
	StopLoss   float64
	TakeProfit float64
	extraMtx   *sync.RWMutex
	extra      map[interface{}]interface{}
	closedOnce *sync.Once
	closed     chan struct{}
}

var positionIDCounter int64

func NewPosition(action OpenPositionAction, openTime time.Time, openPrice float64) (*Position, error) {
	if !action.IsValid() {
		return nil, ErrActionNotValid
	}
	var stopLoss, takeProfit float64
	if action.StopLossIndent != 0 {
		stopLoss = openPrice - action.StopLossIndent*action.Type.Multiplier()
	}
	if action.TakeProfitIndent != 0 {
		takeProfit = openPrice + action.TakeProfitIndent*action.Type.Multiplier()
	}
	return &Position{
		ID:         PositionID(atomic.AddInt64(&positionIDCounter, 1)),
		Type:       action.Type,
		OpenTime:   openTime,
		OpenPrice:  openPrice,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		extraMtx:   &sync.RWMutex{},
		extra:      make(map[interface{}]interface{}),
		closed:     make(chan struct{}),
		closedOnce: &sync.Once{},
	}, nil
}

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

func (p Position) Closed() <-chan struct{} {
	return p.closed
}

func (p Position) IsLong() bool {
	return p.Type == LongPosition
}

func (p Position) IsShort() bool {
	return p.Type == ShortPosition
}

func (p Position) Profit() float64 {
	return p.ProfitByPrice(p.ClosePrice)
}

func (p Position) ProfitByPrice(price float64) float64 {
	return (price - p.OpenPrice) * p.Type.Multiplier()
}

func (p Position) Duration() time.Duration {
	return p.CloseTime.Sub(p.OpenTime)
}

func (p Position) Extra(key interface{}) interface{} {
	p.extraMtx.RLock()
	defer p.extraMtx.RUnlock()
	return p.extra[key]
}

func (p *Position) AddExtra(key interface{}, val interface{}) *Position {
	p.extraMtx.Lock()
	defer p.extraMtx.Unlock()
	p.extra[key] = val
	return p
}

type OpenPositionAction struct {
	Type             PositionType
	StopLossIndent   float64
	TakeProfitIndent float64
	result           chan OpenPositionActionResult
}

func (a *OpenPositionAction) IsValid() bool {
	return a.Type == LongPosition || a.Type == ShortPosition
}

type OpenPositionActionResult struct {
	Position Position
	Closed   PositionClosed
	Error    error
}

func NewOpenPositionAction(pType PositionType, stopLossIndent, takeProfitIndent float64) OpenPositionAction {
	return OpenPositionAction{
		Type:             pType,
		StopLossIndent:   stopLossIndent,
		TakeProfitIndent: takeProfitIndent,
		result:           make(chan OpenPositionActionResult),
	}
}

func (a *OpenPositionAction) Result() <-chan OpenPositionActionResult {
	return a.result
}

type ClosePositionAction struct {
	PositionID PositionID
	result     chan ClosePositionActionResult
}

func NewClosePositionAction(positionID PositionID) ClosePositionAction {
	return ClosePositionAction{
		PositionID: positionID,
		result:     make(chan ClosePositionActionResult),
	}
}

type ClosePositionActionResult struct {
	Position Position
	Error    error
}

func (a *ClosePositionAction) Result() <-chan ClosePositionActionResult {
	return a.result
}

type ChangeConditionalOrderAction struct {
	PositionID PositionID
	StopLoss   float64
	TakeProfit float64 // если 0, то не меняем
	result     chan ChangeConditionalOrderActionResult
}

func (a *ChangeConditionalOrderAction) Result() <-chan ChangeConditionalOrderActionResult {
	return a.result
}

type ChangeConditionalOrderActionResult struct {
	Position Position
	Error    error
}

func NewChangeConditionalOrderAction(positionID PositionID, stopLoss, takeProfit float64) ChangeConditionalOrderAction {
	return ChangeConditionalOrderAction{
		PositionID: positionID,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		result:     make(chan ChangeConditionalOrderActionResult),
	}
}

type Actions <-chan interface{}

//go:generate docker run -v ${PWD}:/app -w /app/ vektra/mockery --name Strategy --inpackage --case snake

type Strategy interface {
	Run(ctx context.Context)
	Actions() Actions
	Errors() <-chan error
}

type PositionClosed <-chan Position

//go:generate docker run -v ${PWD}:/app -w /app/ vektra/mockery --name Broker --inpackage --case snake

type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}

type Engine struct {
	strategy                  Strategy
	broker                    Broker
	onPositionOpened          func(position Position)
	onPositionClosed          func(position Position)
	onConditionalOrderChanged func(position Position)
	sendResultTimeout         time.Duration
	waitGroup                 sync.WaitGroup
}

func New(strategy Strategy, broker Broker) *Engine {
	return &Engine{
		strategy:          strategy,
		broker:            broker,
		sendResultTimeout: 5 * time.Second,
	}
}

func (e *Engine) Run(ctx context.Context) (err error) {
	ctx, cancel := context.WithCancel(ctx)

	e.waitGroup.Add(2)
	go func() {
		defer e.waitGroup.Done()
		defer cancel()
		e.strategy.Run(ctx)
	}()

	go func() {
		defer e.waitGroup.Done()
		defer cancel()
		err = e.run(ctx)
	}()

	e.waitGroup.Wait()
	return
}

func (e *Engine) run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-e.strategy.Errors():
			return err
		case action, ok := <-e.strategy.Actions():
			if !ok {
				return nil
			}
			switch action := action.(type) {
			case OpenPositionAction:
				if err := e.doOpenPosition(ctx, action); err != nil {
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

// OnPositionOpened не потокобезопасно
func (e *Engine) OnPositionOpened(f func(position Position)) *Engine {
	e.onPositionOpened = f
	return e
}

func (e *Engine) OnConditionalOrderChanged(f func(position Position)) *Engine {
	e.onConditionalOrderChanged = f
	return e
}

func (e *Engine) OnPositionClosed(f func(position Position)) *Engine {
	e.onPositionClosed = f
	return e
}

func (e *Engine) doOpenPosition(ctx context.Context, action OpenPositionAction) error {
	position, closed, err := e.broker.OpenPosition(ctx, action)
	closed1, closed2 := e.teePositionClosed(ctx.Done(), closed)
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(e.sendResultTimeout):
		return fmt.Errorf("open position: %w", ErrSendResultTimeout)
	case action.result <- OpenPositionActionResult{
		Position: position,
		Closed:   closed1,
		Error:    err,
	}:
	}

	e.waitGroup.Add(1)
	go func() {
		defer e.waitGroup.Done()
		select {
		case <-ctx.Done():
			return
		case position, ok := <-closed2:
			if !ok {
				return
			}
			if e.onPositionClosed != nil {
				e.onPositionClosed(position)
			}
			return
		}
	}()

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
		Error:    err,
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
		Error:    err,
	}:
	}

	if e.onConditionalOrderChanged != nil {
		e.onConditionalOrderChanged(position)
	}
	return nil
}

func (e *Engine) teePositionClosed(done <-chan struct{}, in PositionClosed) (PositionClosed, PositionClosed) {
	out1 := make(chan Position)
	out2 := make(chan Position)

	e.waitGroup.Add(1)
	go func() {
		defer e.waitGroup.Done()
		defer close(out1)
		defer close(out2)
		for {
			select {
			case <-done:
				return
			case val, ok := <-in:
				if !ok {
					return
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
	}()
	return out1, out2
}
