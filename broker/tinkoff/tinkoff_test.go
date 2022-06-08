package tinkoff

import (
	"testing"

	"bou.ke/monkey"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	investapi "github.com/tinkoff/invest-api-go-sdk"
	"go.uber.org/zap"
	"golang.org/x/net/context"

	"github.com/evsamsonov/trengin"
)

func TestTinkoff_OpenPosition(t *testing.T) {
	ordersServiceClient := &mockOrdersServiceClient{}
	stopOrdersServiceClient := &mockStopOrdersServiceClient{}

	monkey.Patch(uuid.New, func() uuid.UUID {
		return uuid.MustParse("8942e9ae-e4e1-11ec-8fea-0242ac120002")
	})

	tinkoff := &Tinkoff{
		accountID:       "123",
		orderClient:     ordersServiceClient,
		stopOrderClient: stopOrdersServiceClient,
		instrumentFIGI:  "FUTSBRF06220",
		tradedQuantity:  2,
		instrument: &investapi.Instrument{
			MinPriceIncrement: &investapi.Quotation{
				Units: 0,
				Nano:  0.1 * 10e8,
			},
		},
		currentPosition: &currentPosition{},
		logger:          zap.NewNop(),
	}

	ordersServiceClient.On("PostOrder", mock.Anything, &investapi.PostOrderRequest{
		Figi:      "FUTSBRF06220",
		Quantity:  2,
		Direction: investapi.OrderDirection_ORDER_DIRECTION_BUY,
		AccountId: "123",
		OrderType: investapi.OrderType_ORDER_TYPE_MARKET,
		OrderId:   "8942e9ae-e4e1-11ec-8fea-0242ac120002",
	}).Return(&investapi.PostOrderResponse{
		ExecutionReportStatus: investapi.OrderExecutionReportStatus_EXECUTION_REPORT_STATUS_FILL,
		ExecutedOrderPrice: &investapi.MoneyValue{
			Units: 148,
			Nano:  0.2 * 10e8,
		},
	}, nil)

	stopOrdersServiceClient.On("PostStopOrder", mock.Anything, &investapi.PostStopOrderRequest{
		Figi:     "FUTSBRF06220",
		Quantity: 2,
		StopPrice: &investapi.Quotation{
			Units: 136,
			Nano:  0.7 * 10e8,
		},
		Direction:      investapi.StopOrderDirection_STOP_ORDER_DIRECTION_SELL,
		AccountId:      "123",
		ExpirationType: investapi.StopOrderExpirationType_STOP_ORDER_EXPIRATION_TYPE_GOOD_TILL_CANCEL,
		StopOrderType:  investapi.StopOrderType_STOP_ORDER_TYPE_STOP_LOSS,
	}).Return(&investapi.PostStopOrderResponse{
		StopOrderId: "123",
	}, nil)

	position, positionClosed, err := tinkoff.OpenPosition(context.Background(), trengin.OpenPositionAction{
		Type:             trengin.Long,
		StopLossIndent:   11.5,
		TakeProfitIndent: 0,
	})
	if err != nil {
		return
	}

	_ = positionClosed

	assert.Equal(t, trengin.Long, position.Type)
	assert.Equal(t, 148.2, position.OpenPrice)
	assert.Equal(t, 136.7, position.StopLoss)

	assert.Equal(t, "123", tinkoff.currentPosition.StopLossID())
	//assert.Equal(t, 148.2, position.TakeProfit)
}

func TestTinkoff_stopLossPriceByOpen(t *testing.T) {
	tests := []struct {
		name      string
		openPrice *investapi.MoneyValue
		action    trengin.OpenPositionAction
		want      *investapi.Quotation
	}{
		{
			name: "long nano is zero",
			openPrice: &investapi.MoneyValue{
				Units: 123,
				Nano:  0,
			},
			action: trengin.OpenPositionAction{
				Type:           trengin.Long,
				StopLossIndent: 5,
			},
			want: &investapi.Quotation{
				Units: 118,
				Nano:  0,
			},
		},
		{
			name: "long nano without overflow",
			openPrice: &investapi.MoneyValue{
				Units: 123,
				Nano:  430000000,
			},
			action: trengin.OpenPositionAction{
				Type:           trengin.Long,
				StopLossIndent: 50.5,
			},
			want: &investapi.Quotation{
				Units: 72,
				Nano:  930000000,
			},
		},
		{
			name: "long nano with overflow",
			openPrice: &investapi.MoneyValue{
				Units: 123,
				Nano:  530000000,
			},
			action: trengin.OpenPositionAction{
				Type:           trengin.Long,
				StopLossIndent: 50.556,
			},
			want: &investapi.Quotation{
				Units: 72,
				Nano:  974000000,
			},
		},
		{
			name: "short nano is zero",
			openPrice: &investapi.MoneyValue{
				Units: 123,
				Nano:  0,
			},
			action: trengin.OpenPositionAction{
				Type:           trengin.Short,
				StopLossIndent: 5,
			},
			want: &investapi.Quotation{
				Units: 128,
				Nano:  0,
			},
		},
		{
			name: "short nano without overflow",
			openPrice: &investapi.MoneyValue{
				Units: 123,
				Nano:  430000000,
			},
			action: trengin.OpenPositionAction{
				Type:           trengin.Short,
				StopLossIndent: 50.4,
			},
			want: &investapi.Quotation{
				Units: 173,
				Nano:  830000000,
			},
		},
		{
			name: "short nano with overflow",
			openPrice: &investapi.MoneyValue{
				Units: 123,
				Nano:  530000000,
			},
			action: trengin.OpenPositionAction{
				Type:           trengin.Short,
				StopLossIndent: 50.556,
			},
			want: &investapi.Quotation{
				Units: 174,
				Nano:  86000000,
			},
		},
	}

	tinkoff := Tinkoff{
		instrument: &investapi.Instrument{
			MinPriceIncrement: &investapi.Quotation{
				Nano: 1000000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openPrice := NewMoneyValue(tt.openPrice)
			quotation := tinkoff.stopLossPriceByOpen(openPrice, tt.action)
			assert.Equal(t, tt.want, quotation)
		})
	}
}
