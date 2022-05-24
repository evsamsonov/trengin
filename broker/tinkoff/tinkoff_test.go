package tinkoff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	investapi "github.com/tinkoff/invest-api-go-sdk"

	"github.com/evsamsonov/trengin"
)

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
