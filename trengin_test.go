package trengin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"golang.org/x/sync/errgroup"
)

func TestPositionType_Multiplier(t *testing.T) {
	tests := []struct {
		name         string
		positionType PositionType
		want         float64
	}{
		{
			name:         "long",
			positionType: Long,
			want:         1,
		},
		{
			name:         "short",
			positionType: Short,
			want:         -1,
		},
		{
			name:         "unexpected",
			positionType: PositionType(0),
			want:         0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.positionType.Multiplier())
		})
	}
}

func TestPositionType_NewPosition(t *testing.T) {
	tests := []struct {
		name      string
		action    OpenPositionAction
		openPrice float64
		openTime  time.Time
		want      *Position
		wantErr   error
	}{
		{
			name:      "action not valid",
			action:    OpenPositionAction{},
			openPrice: 10,
			openTime:  time.Unix(1, 0),
			want:      nil,
			wantErr:   ErrActionNotValid,
		},
		{
			name: "long",
			action: OpenPositionAction{
				Type:             Long,
				Quantity:         1,
				StopLossIndent:   1,
				TakeProfitIndent: 2,
				result:           make(chan OpenPositionActionResult),
			},
			openPrice: 10,
			openTime:  time.Unix(1, 0),
			want: &Position{
				ID:         PositionID(uuid.New()),
				Type:       Long,
				OpenTime:   time.Unix(1, 0),
				OpenPrice:  10,
				CloseTime:  time.Time{},
				StopLoss:   9,
				TakeProfit: 12,
			},
			wantErr: nil,
		},
		{
			name: "short",
			action: OpenPositionAction{
				Type:             Short,
				Quantity:         1,
				StopLossIndent:   1,
				TakeProfitIndent: 2,
				result:           make(chan OpenPositionActionResult),
			},
			openPrice: 10,
			openTime:  time.Unix(1, 0),
			want: &Position{
				ID:         PositionID(uuid.New()),
				Type:       Short,
				OpenTime:   time.Unix(1, 0),
				OpenPrice:  10,
				CloseTime:  time.Time{},
				StopLoss:   11,
				TakeProfit: 8,
			},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			position, err := NewPosition(tt.action, tt.openTime, tt.openPrice)
			if err != nil {
				assert.Equal(t, tt.wantErr, err)
				assert.Nil(t, position)
				return
			}
			assert.Equal(t, tt.want.Type, position.Type)
			assert.Equal(t, tt.want.OpenTime, position.OpenTime)
			assert.Equal(t, tt.want.OpenPrice, position.OpenPrice)
			assert.Equal(t, tt.want.CloseTime, position.CloseTime)
			assert.Equal(t, tt.want.StopLoss, position.StopLoss)
			assert.Equal(t, tt.want.TakeProfit, position.TakeProfit)
		})
	}
}

func TestPosition_Close(t *testing.T) {
	t.Run("close once", func(t *testing.T) {
		position := Position{
			closedOnce: &sync.Once{},
			closed:     make(chan struct{}),
		}

		closeTime := time.Unix(1, 0)
		err := position.Close(closeTime, 12)
		assert.Nil(t, err)
		assert.Equal(t, 12., position.ClosePrice)
		assert.Equal(t, closeTime, position.CloseTime)
		select {
		case <-position.Closed():
		default:
			assert.Fail(t, "position not closed")
		}
	})

	t.Run("close twice", func(t *testing.T) {
		position := Position{
			closedOnce: &sync.Once{},
			closed:     make(chan struct{}),
		}

		closeTime := time.Unix(1, 0)
		err := position.Close(closeTime, 12)
		assert.Nil(t, err)
		err = position.Close(closeTime, 14)
		assert.Equal(t, ErrAlreadyClosed, err)
		assert.Equal(t, 12., position.ClosePrice)
		assert.Equal(t, closeTime, position.CloseTime)
	})
}

func TestPosition_IsLong(t *testing.T) {
	position := Position{Type: Long}
	assert.True(t, position.IsLong())
}

func TestPosition_IsShort(t *testing.T) {
	position := Position{Type: Short}
	assert.True(t, position.IsShort())
}

func TestPosition_Profit(t *testing.T) {
	tests := []struct {
		name     string
		position Position
		want     float64
	}{
		{
			name:     "long",
			position: Position{Type: Long, Quantity: 1, OpenPrice: 10, ClosePrice: 15},
			want:     5,
		},
		{
			name:     "short",
			position: Position{Type: Short, Quantity: 1, OpenPrice: 10, ClosePrice: 15},
			want:     -5,
		},
		{
			name:     "short, quantity=5",
			position: Position{Type: Short, Quantity: 5, OpenPrice: 10, ClosePrice: 15},
			want:     -25,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.position.Profit())
		})
	}
}

func TestPosition_ProfitByPrice(t *testing.T) {
	tests := []struct {
		name     string
		position Position
		price    float64
		want     float64
	}{
		{
			name:     "long",
			position: Position{Type: Long, Quantity: 1, OpenPrice: 10},
			price:    25,
			want:     15,
		},
		{
			name:     "short",
			position: Position{Type: Short, Quantity: 1, OpenPrice: 10},
			price:    5,
			want:     5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.position.ProfitByPrice(tt.price))
		})
	}
}

func TestPosition_Duration(t *testing.T) {
	position := Position{OpenTime: time.Unix(1, 0), CloseTime: time.Unix(10, 0)}
	assert.Equal(t, position.Duration(), 9*time.Second)
}

func TestPosition_Extra(t *testing.T) {
	position := Position{
		extraMtx: &sync.RWMutex{},
		extra:    make(map[interface{}]interface{}),
	}
	assert.Nil(t, position.Extra("test"))
	position.SetExtra("test", 123)
	assert.Equal(t, 123, position.Extra("test"))
}

func TestPosition_RangeExtra(t *testing.T) {
	position := Position{
		extraMtx: &sync.RWMutex{},
		extra:    make(map[interface{}]interface{}),
	}

	position.SetExtra("key1", "value1")
	position.SetExtra("key2", "value2")
	position.SetExtra("key3", "value3")
	position.RangeExtra(func(key interface{}, val interface{}) {
		fmt.Printf("%s: %s\n", key, val)
	})
	// Output:
	// key1: value1
	// key2: value2
	// key3: value3
}

func TestOpenPositionAction_IsValid(t *testing.T) {
	t.Run("not valid", func(t *testing.T) {
		action := OpenPositionAction{Type: 0}
		assert.False(t, action.IsValid())
	})

	t.Run("valid", func(t *testing.T) {
		action := OpenPositionAction{Type: Long, Quantity: 1}
		assert.True(t, action.IsValid())
	})
}

func TestPosition_IsClosed(t *testing.T) {
	t.Run("not closed", func(t *testing.T) {
		position := Position{closed: make(chan struct{})}
		assert.False(t, position.IsClosed())
	})

	t.Run("closed", func(t *testing.T) {
		position := Position{
			closed:     make(chan struct{}),
			closedOnce: &sync.Once{},
		}
		assert.NoError(t, position.Close(time.Now(), 123))
		assert.True(t, position.IsClosed())
	})
}

func TestEngine_doOpenPosition(t *testing.T) {
	broker := &MockBroker{}
	position := Position{}
	closedPosition := Position{}
	positionClosed := make(chan Position)

	var onPositionOpenedCalled bool
	var onPositionClosedCalled int64
	engine := Engine{
		broker: broker,
		onPositionOpened: func(p Position) {
			assert.Equal(t, position, p)
			onPositionOpenedCalled = true
		},
		onPositionClosed: func(p Position) {
			assert.Equal(t, position, p)
			atomic.AddInt64(&onPositionClosedCalled, 1)
		},
		sendResultTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	resultChan := make(chan OpenPositionActionResult, 1)
	action := OpenPositionAction{result: resultChan}
	broker.On("OpenPosition", ctx, action).Return(position, PositionClosed(positionClosed), nil)

	g := &errgroup.Group{}
	err := engine.doOpenPosition(ctx, g, action)
	assert.Nil(t, err)
	result := <-resultChan
	assert.Equal(t, position, result.Position)
	assert.Nil(t, result.error)

	positionClosed <- closedPosition
	assert.True(t, onPositionOpenedCalled)

	timeout := time.After(100 * time.Millisecond)
waitCalledLoop:
	for {
		select {
		case <-timeout:
			assert.Fail(t, "onPositionClosed not called")
			break waitCalledLoop
		default:
			if atomic.LoadInt64(&onPositionClosedCalled) == 1 {
				break waitCalledLoop
			}
		}
	}
	cancel()
	g.Wait()
}

func TestEngine_doClosePosition(t *testing.T) {
	broker := &MockBroker{}
	position := Position{}
	engine := Engine{
		broker:            broker,
		sendResultTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultChan := make(chan ClosePositionActionResult, 1)
	action := ClosePositionAction{result: resultChan}
	broker.On("ClosePosition", ctx, action).Return(position, nil)

	err := engine.doClosePosition(ctx, action)
	assert.Nil(t, err)
	result := <-resultChan
	assert.Equal(t, position, result.Position)
	assert.Nil(t, result.error)
}

func TestEngine_doChangeConditionalOrder(t *testing.T) {
	broker := &MockBroker{}
	position := Position{}

	var onChangeConditionalOrderCalled bool
	engine := Engine{
		broker:            broker,
		sendResultTimeout: 5 * time.Second,
		onConditionalOrderChanged: func(p Position) {
			assert.Equal(t, position, p)
			onChangeConditionalOrderCalled = true
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultChan := make(chan ChangeConditionalOrderActionResult, 1)
	action := ChangeConditionalOrderAction{result: resultChan}
	broker.On("ChangeConditionalOrder", ctx, action).Return(position, nil)

	err := engine.doChangeConditionalOrder(ctx, action)
	assert.Nil(t, err)
	result := <-resultChan
	assert.Equal(t, position, result.Position)
	assert.Nil(t, result.error)
	assert.True(t, onChangeConditionalOrderCalled)
}

func TestEngine_Run(t *testing.T) {
	t.Run("context canceled", func(t *testing.T) {
		strategy := &MockStrategy{}
		broker := &MockBroker{}
		ctx, cancel := context.WithCancel(context.Background())

		strategy.On("Run", mock.Anything).After(100 * time.Millisecond).Return(nil)
		strategy.On("Actions").Return(make(Actions))

		broker.On("Run", mock.Anything).After(100 * time.Millisecond).Return(nil)

		engine := Engine{
			strategy: strategy,
			broker:   broker,
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := engine.Run(ctx)
			assert.ErrorIs(t, err, context.Canceled)
		}()
		cancel()
		wg.Wait()
	})

	t.Run("error received", func(t *testing.T) {
		strategy := &MockStrategy{}
		broker := &MockBroker{}
		ctx := context.Background()

		strategy.On("Run", mock.Anything).After(1000 * time.Millisecond).Return(nil)
		strategy.On("Actions").Return(make(Actions))

		expectedErr := errors.New("error")
		broker.On("Run", mock.Anything).After(1000 * time.Millisecond).Return(expectedErr)

		engine := Engine{
			strategy: strategy,
			broker:   broker,
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := engine.Run(ctx)
			assert.ErrorIs(t, err, expectedErr)
		}()

		wg.Wait()
	})

	t.Run("unknown action", func(t *testing.T) {
		strategy := &MockStrategy{}
		broker := &MockBroker{}
		ctx := context.Background()

		actionsChan := make(chan interface{})
		var actionsReadChan Actions //nolint: gosimple
		actionsReadChan = actionsChan
		strategy.On("Run", mock.Anything).After(100 * time.Millisecond).Return(nil)
		strategy.On("Actions").Return(actionsReadChan)

		broker.On("Run", mock.Anything).After(100 * time.Millisecond).Return(nil)

		engine := Engine{
			strategy: strategy,
			broker:   broker,
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := engine.Run(ctx)
			assert.ErrorIs(t, err, ErrUnknownAction)
		}()

		actionsChan <- "unknown action"
		wg.Wait()
	})
}
