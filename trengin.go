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

//go:generate docker run --rm -v ${PWD}:/app -w /app/ vektra/mockery --name Strategy --inpackage --case snake

// Strategy описывает интерфейс торговой стратегии. Позволяет реализовать стратегию,
// взаимодействуя с Engine через каналы, которые возвращают методы Actions и Errors.
// Actions используется для отправки торговых действий, Errors для оповещения
// о критической ошибке. Есть закрыть любой из каналов или отправить значение
// в канал ошибок, то Engine завершит свою работу
type Strategy interface {
	// Run запускает стратегию в работу
	Run(ctx context.Context)

	// Actions возвращает канал для получения торговых действий. При закрытии
	// канала Engine завершит работу.
	Actions() Actions

	// Errors возвращает канал для получения ошибок. При получении сообщения
	// из канала или на его закрытие Engine завершит работу.
	Errors() <-chan error
}

// Actions это канал для передачи торговых действий от Strategy к Broker
// Может принимать типы OpenPositionAction, ClosePositionAction, ChangeConditionalOrderAction.
// Неожиданные типы приведут к ошибке и завершению работы Engine
type Actions <-chan interface{}

//go:generate docker run --rm -v ${PWD}:/app -w /app/ vektra/mockery --name Broker --inpackage --case snake

// Broker описывает интерфейс клиента, исполняющего торговые операции
// и отслеживающего статус условных заявок по позициям.
type Broker interface {
	// OpenPosition открывает позицию и запускает отслеживание условной заявки
	// Возвращает открытую позицию, и канал PositionClosed, в который будет отправлена
	// позиция при закрытии.
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)

	// ClosePosition закрывает позицию. Возвращает закрытую позицию
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)

	// ChangeConditionalOrder изменяет условную заявку по позиции. Возвращает измененную позицию
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}

// PositionClosed канал, в который отправляется позиция при закрытии
type PositionClosed <-chan Position

// Position описывает торговую позицию. Идентификатор ID является уникальным
// только в рамках одного запуска
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

// NewPosition создает новую позицию по action, с временем открытия openTime
// и с ценой открытия openPrice. Если action невалиден, то вернет ErrActionNotValid.
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

func (p *Position) IsLong() bool {
	return p.Type == Long
}

func (p *Position) IsShort() bool {
	return p.Type == Short
}

// Profit возвращает прибыль по закрытой сделке. Для получения незафиксированной прибыли
// по открытой позиции следует использовать метод ProfitByPrice
func (p *Position) Profit() float64 {
	return p.ProfitByPrice(p.ClosePrice)
}

// ProfitByPrice возвращает прибыль позиции при указанной цене price
func (p *Position) ProfitByPrice(price float64) float64 {
	return (price - p.OpenPrice) * p.Type.Multiplier()
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

// OpenPositionAction описывает действие по открытию позиции с типом Type и отступами
// условной заявки StopLossIndent и TakeProfitIndent
type OpenPositionAction struct {
	Type PositionType

	// Отступ стоп-лосса от цены открытия. Если равен 0, то стоп-лосс не должен использоваться
	StopLossIndent float64

	// Отступ тейк-профита от цены открытия. Если равен 0, то тейк-профит не должен использоваться
	TakeProfitIndent float64

	result chan OpenPositionActionResult
}

// IsValid проверяет, что действие валидно
func (a *OpenPositionAction) IsValid() bool {
	return a.Type == Long || a.Type == Short
}

// OpenPositionActionResult результат открытия позиции
type OpenPositionActionResult struct {
	Position Position
	Closed   PositionClosed // Канал, для отслеживания закрытия сделки
	error    error
}

// NewOpenPositionAction создает действие на открытие позиции с типом positionType,
// отступом стоп-лосса от цены открытия stopLossIndent и отступом тейк-профита
// от цены открытия takeProfitIndent. Если стоп-лосс или тейк-профит не требуются,
// то соответствующие значения отступов должны быть равны 0.
func NewOpenPositionAction(positionType PositionType, stopLossIndent, takeProfitIndent float64) OpenPositionAction {
	return OpenPositionAction{
		Type:             positionType,
		StopLossIndent:   stopLossIndent,
		TakeProfitIndent: takeProfitIndent,
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

// Engine описывыет торговый движок. Создавать следует через конструктор New
type Engine struct {
	strategy                  Strategy
	broker                    Broker
	onPositionOpened          func(position Position)
	onPositionClosed          func(position Position)
	onConditionalOrderChanged func(position Position)
	sendResultTimeout         time.Duration
	waitGroup                 sync.WaitGroup
}

// New создает экземпляр Engine и возвращает указатель на него
func New(strategy Strategy, broker Broker) *Engine {
	return &Engine{
		strategy:          strategy,
		broker:            broker,
		sendResultTimeout: 1 * time.Second,
	}
}

// Run запускает стратегию в работу
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
			return ctx.Err()
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
		error:    err,
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
