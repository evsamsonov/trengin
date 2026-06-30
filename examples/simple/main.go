package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/evsamsonov/trengin/v2"
)

const figi = "BBG004730N88"

func main() {
	ctx := context.Background()

	strategy := demoStrategy{}
	broker := newMemoryBroker(100, 108)
	engine := trengin.New(strategy, broker)

	engine.
		OnPositionOpened(func(position trengin.Position) {
			fmt.Printf(
				"opened: figi=%s price=%.2f stop=%.2f take=%.2f\n",
				position.FIGI,
				position.OpenPrice,
				position.StopLoss,
				position.TakeProfit,
			)
		}).
		OnConditionalOrderChanged(func(position trengin.Position) {
			fmt.Printf(
				"changed: figi=%s stop=%.2f take=%.2f\n",
				position.FIGI,
				position.StopLoss,
				position.TakeProfit,
			)
		}).
		OnPositionClosed(func(position trengin.Position) {
			fmt.Printf(
				"closed: figi=%s price=%.2f profit=%.2f\n",
				position.FIGI,
				position.ClosePrice,
				position.Profit(),
			)
		})

	if err := engine.Run(ctx); err != nil {
		panic(err)
	}
}

type demoStrategy struct{}

func (s demoStrategy) Run(ctx context.Context, actions trengin.Actions) error {
	defer close(actions)

	openAction := trengin.NewOpenPositionAction(figi, trengin.Long, 1, 5, 10)
	if err := sendAction(ctx, actions, openAction); err != nil {
		return err
	}

	openResult, err := openAction.Result(ctx)
	if err != nil {
		return err
	}

	changeAction := trengin.NewChangeConditionalOrderAction(
		openResult.Position.ID,
		openResult.Position.OpenPrice-3,
		openResult.Position.OpenPrice+12,
	)
	if err := sendAction(ctx, actions, changeAction); err != nil {
		return err
	}
	if _, err := changeAction.Result(ctx); err != nil {
		return err
	}

	closeAction := trengin.NewClosePositionAction(openResult.Position.ID)
	if err := sendAction(ctx, actions, closeAction); err != nil {
		return err
	}
	if _, err := closeAction.Result(ctx); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-openResult.Closed:
		return nil
	}
}

func sendAction(ctx context.Context, actions trengin.Actions, action interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case actions <- action:
		return nil
	}
}

type memoryBroker struct {
	mu         sync.Mutex
	openPrice  float64
	closePrice float64
	positions  map[trengin.PositionID]*memoryPosition
}

type memoryPosition struct {
	position trengin.Position
	closed   chan trengin.Position
}

func newMemoryBroker(openPrice, closePrice float64) *memoryBroker {
	return &memoryBroker{
		openPrice:  openPrice,
		closePrice: closePrice,
		positions:  make(map[trengin.PositionID]*memoryPosition),
	}
}

func (b *memoryBroker) OpenPosition(
	ctx context.Context,
	action trengin.OpenPositionAction,
) (trengin.Position, trengin.PositionClosed, error) {
	select {
	case <-ctx.Done():
		return trengin.Position{}, nil, ctx.Err()
	default:
	}

	position, err := trengin.NewPosition(action, time.Now(), b.openPrice)
	if err != nil {
		return trengin.Position{}, nil, err
	}

	closed := make(chan trengin.Position, 1)

	b.mu.Lock()
	b.positions[position.ID] = &memoryPosition{
		position: *position,
		closed:   closed,
	}
	b.mu.Unlock()

	return *position, closed, nil
}

func (b *memoryBroker) ChangeConditionalOrder(
	ctx context.Context,
	action trengin.ChangeConditionalOrderAction,
) (trengin.Position, error) {
	select {
	case <-ctx.Done():
		return trengin.Position{}, ctx.Err()
	default:
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	memoryPosition, ok := b.positions[action.PositionID]
	if !ok {
		return trengin.Position{}, errors.New("position not found")
	}

	if action.StopLoss != 0 {
		memoryPosition.position.StopLoss = action.StopLoss
	}
	if action.TakeProfit != 0 {
		memoryPosition.position.TakeProfit = action.TakeProfit
	}

	return memoryPosition.position, nil
}

func (b *memoryBroker) ClosePosition(
	ctx context.Context,
	action trengin.ClosePositionAction,
) (trengin.Position, error) {
	select {
	case <-ctx.Done():
		return trengin.Position{}, ctx.Err()
	default:
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	memoryPosition, ok := b.positions[action.PositionID]
	if !ok {
		return trengin.Position{}, errors.New("position not found")
	}

	if err := memoryPosition.position.Close(time.Now(), b.closePrice); err != nil {
		return trengin.Position{}, err
	}

	memoryPosition.closed <- memoryPosition.position
	close(memoryPosition.closed)
	delete(b.positions, action.PositionID)

	return memoryPosition.position, nil
}
