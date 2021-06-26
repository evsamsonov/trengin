package trengin

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPositionType_Multiplier(t *testing.T) {
	tests := []struct {
		name         string
		positionType PositionType
		want         float64
	}{
		{
			name:         "long",
			positionType: LongPosition,
			want:         1,
		},
		{
			name:         "short",
			positionType: ShortPosition,
			want:         -1,
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
				Type:             LongPosition,
				StopLossIndent:   1,
				TakeProfitIndent: 2,
				result:           make(chan OpenPositionActionResult),
			},
			openPrice: 10,
			openTime:  time.Unix(1, 0),
			want: &Position{
				ID:         1,
				Type:       LongPosition,
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
				Type:             ShortPosition,
				StopLossIndent:   1,
				TakeProfitIndent: 2,
				result:           make(chan OpenPositionActionResult),
			},
			openPrice: 10,
			openTime:  time.Unix(1, 0),
			want: &Position{
				ID:         1,
				Type:       ShortPosition,
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
	position := Position{Type: LongPosition}
	assert.True(t, position.IsLong())
}

func TestPosition_IsShort(t *testing.T) {
	position := Position{Type: ShortPosition}
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
			position: Position{Type: LongPosition, OpenPrice: 10, ClosePrice: 15},
			want:     5,
		},
		{
			name:     "short",
			position: Position{Type: ShortPosition, OpenPrice: 10, ClosePrice: 15},
			want:     -5,
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
			position: Position{Type: LongPosition, OpenPrice: 10},
			price:    25,
			want:     15,
		},
		{
			name:     "short",
			position: Position{Type: ShortPosition, OpenPrice: 10},
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
	position.AddExtra("test", 123)
	assert.Equal(t, 123, position.Extra("test"))
}

func TestOpenPositionAction_IsValid(t *testing.T) {
	t.Run("not valid", func(t *testing.T) {
		action := OpenPositionAction{Type: 0}
		assert.False(t, action.IsValid())
	})

	t.Run("valid", func(t *testing.T) {
		action := OpenPositionAction{Type: LongPosition}
		assert.True(t, action.IsValid())
	})
}

func TestEngine_doOpenPosition(t *testing.T) {
	strategy := &MockStrategy{}
	broker := &MockBroker{}
	position := Position{}
	closedPosition := Position{}
	positionClosed := make(chan Position)

	var onPositionOpenedCalled, onPositionClosedCalled bool
	engine := Engine{
		strategy: strategy,
		broker:   broker,
		onPositionOpened: func(p Position) {
			assert.Equal(t, position, p)
			onPositionOpenedCalled = true
		},
		onPositionClosed: func(p Position) {
			assert.Equal(t, position, p)
			onPositionClosedCalled = true
		},
		sendResultTimeout: 5 * time.Second,
	}

	ctx := context.Background()
	resultChan := make(chan OpenPositionActionResult, 1)
	action := OpenPositionAction{result: resultChan}
	broker.On("OpenPosition", ctx, action).Return(position, PositionClosed(positionClosed), nil)

	err := engine.doOpenPosition(ctx, action)
	assert.Nil(t, err)
	result := <-resultChan
	assert.Equal(t, position, result.Position)
	assert.Nil(t, result.Error)

	positionClosed <- closedPosition
	assert.True(t, onPositionOpenedCalled)
	assert.True(t, onPositionClosedCalled)
}

func TestEngine_doClosePosition(t *testing.T) {
	strategy := &MockStrategy{}
	broker := &MockBroker{}
	position := Position{}
	engine := Engine{
		strategy:          strategy,
		broker:            broker,
		sendResultTimeout: 5 * time.Second,
	}

	ctx := context.Background()
	resultChan := make(chan ClosePositionActionResult, 1)
	action := ClosePositionAction{result: resultChan}
	broker.On("ClosePosition", ctx, action).Return(position, nil)

	err := engine.doClosePosition(ctx, action)
	assert.Nil(t, err)
	result := <-resultChan
	assert.Equal(t, position, result.Position)
	assert.Nil(t, result.Error)
}

func TestEngine_doChangeConditionalOrder(t *testing.T) {
	strategy := &MockStrategy{}
	broker := &MockBroker{}
	position := Position{}

	var onChangeConditionalOrderCalled bool
	engine := Engine{
		strategy:          strategy,
		broker:            broker,
		sendResultTimeout: 5 * time.Second,
		onConditionalOrderChanged: func(p Position) {
			assert.Equal(t, position, p)
			onChangeConditionalOrderCalled = true
		},
	}

	ctx := context.Background()
	resultChan := make(chan ChangeConditionalOrderActionResult, 1)
	action := ChangeConditionalOrderAction{result: resultChan}
	broker.On("ChangeConditionalOrder", ctx, action).Return(position, nil)

	err := engine.doChangeConditionalOrder(ctx, action)
	assert.Nil(t, err)
	result := <-resultChan
	assert.Equal(t, position, result.Position)
	assert.Nil(t, result.Error)
	assert.True(t, onChangeConditionalOrderCalled)
}
