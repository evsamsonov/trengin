package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/evsamsonov/trengin/v2"
)

var errPositionNotFound = errors.New("position not found")

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

func newMemoryBroker(openPrice float64, closePrice float64) *memoryBroker {
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
		return trengin.Position{}, nil, fmt.Errorf("create position: %w", err)
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

	stored, ok := b.positions[action.PositionID]
	if !ok {
		return trengin.Position{}, fmt.Errorf("find position: %w", errPositionNotFound)
	}

	if action.StopLoss != 0 {
		stored.position.StopLoss = action.StopLoss
	}
	if action.TakeProfit != 0 {
		stored.position.TakeProfit = action.TakeProfit
	}

	return stored.position, nil
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

	stored, ok := b.positions[action.PositionID]
	if !ok {
		return trengin.Position{}, fmt.Errorf("find position: %w", errPositionNotFound)
	}

	if err := stored.position.Close(time.Now(), b.closePrice); err != nil {
		return trengin.Position{}, fmt.Errorf("close position: %w", err)
	}

	stored.closed <- stored.position
	close(stored.closed)
	delete(b.positions, action.PositionID)

	return stored.position, nil
}
