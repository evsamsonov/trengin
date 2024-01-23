// Package trengin предоставляет каркас для создания торгового робота.
// Определяет интерфейс Strategy и Broker, позволяя связать
// реализации этих интерфейсов через экземпляр Engine.
//
// Strategy имеет возможность гибко выполнять действия по открытию новой
// позиции (OpenPositionAction), изменении условной заявки позиции (стоп-лосс и тейк-профит)
// (ChangeConditionalOrderAction) и закрытию позиции (ClosePositionAction).
//
// Broker должен реализовывать функционал открытия сделки, отслеживания статуса условной заявки,
// изменения условной заявки и закрытия позиции.
//
// Для выполнения дополнительного функционала можно устанавливать коллбеки
// на события изменения позиции c помощью методов OnPositionOpened, OnPositionClosed
// и OnConditionalOrderChanged
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

// Multiplier возвращает 1 для значения Long, -1 для значения Short
// и 0 на любое другое значение. Может использоваться как множитель
// при вычислениях, которые зависят от типа позиции, например,
// при вычислении прибыли по позиции
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

// Strategy описывает интерфейс торговой стратегии. Позволяет реализовать стратегию,
// взаимодействуя с Engine через канал, которые возвращает метод Actions.
// Actions используется для отправки торговых действий. Есть закрыть канал Actions,
// то Engine завершит свою работу
type Strategy interface {
	// Run запускает стратегию в работу
	Run(ctx context.Context, actions Actions) error
}

// Actions это канал для передачи торговых действий от Strategy к Broker
// Может принимать типы OpenPositionAction, ClosePositionAction, ChangeConditionalOrderAction.
// Неожиданные типы приведут к ошибке и завершению работы Engine
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

// PositionClosed канал, в который отправляется позиция при закрытии
type PositionClosed <-chan Position

// Position описывает торговую позицию. Идентификатор ID является уникальным
// только в рамках одного запуска

// Position is a trading position.
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

// NewPosition создает новую позицию по action, с временем открытия openTime
// и с ценой открытия openPrice. Если action невалиден, то вернет ErrActionNotValid.
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

// Close закрывает позицию с временем закрытия closeTime и ценой закрытия closePrice.
// При повторном вызове вернет ошибку ErrAlreadyClosed, время и цена закрытия
// в этом случае не изменится.
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

// Closed возвращает канал, который будет закрыт при закрытии позиции
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

// Profit возвращает прибыль по закрытой сделке. Для получения незафиксированной прибыли
// по открытой позиции следует использовать метод ProfitByPrice
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

// ProfitByPrice возвращает прибыль позиции при указанной цене price
func (p *Position) ProfitByPrice(price float64) float64 {
	return (price - p.OpenPrice) * p.Type.Multiplier() * float64(p.Quantity)
}

// Duration возвращает длительность закрытой сделки
func (p *Position) Duration() time.Duration {
	return p.CloseTime.Sub(p.OpenTime)
}

// Extra получает значение дополнительного поля по ключу key.
// Если значение не задано, то вернет nil
func (p *Position) Extra(key interface{}) interface{} {
	p.extraMtx.RLock()
	defer p.extraMtx.RUnlock()
	return p.extra[key]
}

// SetExtra устанавливает значение дополнительного поля с ключом key.
// Может использоваться для хранения дополнительных необязательных информационных
// данных при реализации стратегии или брокера. Не следует завязываться
// на эти данные при реализации логики работы Strategy или Broker.
// Исключение: локальное использование в рамках реализации Strategy или Broker
func (p *Position) SetExtra(key interface{}, val interface{}) *Position {
	p.extraMtx.Lock()
	defer p.extraMtx.Unlock()
	p.extra[key] = val
	return p
}

// RangeExtra применяет функцию f ко всем элементам списка Extra
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

// IsValid проверяет, что действие валидно
func (a *OpenPositionAction) IsValid() bool {
	return a.Type.IsValid() && a.Quantity > 0
}

// OpenPositionActionResult результат открытия позиции
type OpenPositionActionResult struct {
	Position Position
	Closed   PositionClosed // Канал, для отслеживания закрытия сделки
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

// Result возвращает результат выполнения действия на открытие позиции.
func (a *OpenPositionAction) Result(ctx context.Context) (OpenPositionActionResult, error) {
	select {
	case <-ctx.Done():
		return OpenPositionActionResult{}, ctx.Err()
	case result := <-a.result:
		return result, result.error
	}
}

// ClosePositionAction описывает действие по закрытию позиции.
type ClosePositionAction struct {
	PositionID PositionID
	result     chan ClosePositionActionResult
}

// NewClosePositionAction создает действие на закрытие позиции с идентификатором positionID.
func NewClosePositionAction(positionID PositionID) ClosePositionAction {
	return ClosePositionAction{
		PositionID: positionID,
		result:     make(chan ClosePositionActionResult),
	}
}

// ClosePositionActionResult описывает результат закрытия позиции.
type ClosePositionActionResult struct {
	Position Position
	error    error
}

// Result возвращает результат выполнения действия на закрытия позиции.
func (a *ClosePositionAction) Result(ctx context.Context) (ClosePositionActionResult, error) {
	select {
	case <-ctx.Done():
		return ClosePositionActionResult{}, ctx.Err()
	case result := <-a.result:
		return result, result.error
	}
}

// ChangeConditionalOrderAction описывает действие на изменение условной заявки
// позиции с идентификатором PositionID. При передаче StopLoss или TakeProfit
// равным 0 данные значения не должны изменяться.
type ChangeConditionalOrderAction struct {
	PositionID PositionID
	StopLoss   float64
	TakeProfit float64
	result     chan ChangeConditionalOrderActionResult
}

// Result возвращает канал, который вернет результат выполнения действия на изменения условной заявки.
func (a *ChangeConditionalOrderAction) Result(ctx context.Context) (ChangeConditionalOrderActionResult, error) {
	select {
	case <-ctx.Done():
		return ChangeConditionalOrderActionResult{}, ctx.Err()
	case result := <-a.result:
		return result, result.error
	}
}

// ChangeConditionalOrderActionResult описывает результат изменения условной заявки
type ChangeConditionalOrderActionResult struct {
	Position Position
	error    error
}

// NewChangeConditionalOrderAction создает действие на изменение условной заявки по позиции
// с указанным positionID и новыми значения stopLoss и takeProfit. Если менять stopLoss или takeProfit
// не требуется, то нужно передать их равными 0.
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

// Engine описывыет торговый движок. Создавать следует через конструктор New
type Engine struct {
	strategy                  Strategy
	broker                    Broker
	onPositionOpened          func(position Position)
	onPositionClosed          func(position Position)
	onConditionalOrderChanged func(position Position)
	sendResultTimeout         time.Duration
	preventBrokerRun          bool
}

// New создает экземпляр Engine и возвращает указатель на него
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

// Run запускает стратегию в работу
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

// OnPositionOpened устанавливает коллбек f на открытие позиции.
// Актуальная позиция передается параметром в метод f.
// Возвращает указатель на Engine, реализуя текучий интерфейс.
//
// Метод не потокобезопасен. Не следует вызывать в разных горутинах
// и после запуска Engine
func (e *Engine) OnPositionOpened(f func(position Position)) *Engine {
	e.onPositionOpened = f
	return e
}

// OnConditionalOrderChanged устанавливает коллбек f на изменение условной заявки
// по позиции. Актуальная позиция передается параметром в метод f.
// Возвращает указатель на Engine, реализуя текучий интерфейс.
//
// Метод не потокобезопасен. Не следует вызывать в разных горутинах
// и после запуска Engine
func (e *Engine) OnConditionalOrderChanged(f func(position Position)) *Engine {
	e.onConditionalOrderChanged = f
	return e
}

// OnPositionClosed устанавливает коллбек f на закрытие позиции.
// Актуальная позиция передается параметром в метод f.
// Возвращает указатель на Engine, реализуя текучий интерфейс.
//
// Метод не потокобезопасен. Не следует вызывать в разных горутинах
// и после запуска Engine
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
