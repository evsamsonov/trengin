// https://tinkoff.github.io/investAPI/
package tinkoff

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/evsamsonov/trengin"
	"github.com/google/uuid"
	investapi "github.com/tinkoff/invest-api-go-sdk"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const (
	tinkoffHost = "invest-public-api.tinkoff.ru:443"
)

type Tinkoff struct {
	accountID         string
	token             string
	orderClient       investapi.OrdersServiceClient
	stopOrderClient   investapi.StopOrdersServiceClient
	orderStreamClient investapi.OrdersStreamServiceClient
	instrumentFIGI    string
	tradedQuantity    int64
	currentPosition   *currentPosition
	logger            *zap.Logger
}

type Option func(*Tinkoff)

func WithLogger(logger *zap.Logger) Option {
	return func(t *Tinkoff) {
		t.logger = logger
	}
}

func New(token, accountID, instrumentFIGI string, tradedQuantity int64, opts ...Option) (*Tinkoff, error) {
	conn, err := grpc.Dial(
		tinkoffHost,
		grpc.WithTransportCredentials(
			credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}), //nolint: gosec
		),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial: %w", err)
	}

	tinkoff := &Tinkoff{
		accountID:         accountID,
		token:             token,
		instrumentFIGI:    instrumentFIGI,
		tradedQuantity:    tradedQuantity,
		orderClient:       investapi.NewOrdersServiceClient(conn),
		stopOrderClient:   investapi.NewStopOrdersServiceClient(conn),
		orderStreamClient: investapi.NewOrdersStreamServiceClient(conn),
		currentPosition:   &currentPosition{},
		logger:            zap.NewNop(),
	}
	for _, opt := range opts {
		opt(tinkoff)
	}
	return tinkoff, nil
}

func (t *Tinkoff) Run(ctx context.Context) error {
	ctx = t.ctxWithAuth(ctx)
	stream, err := t.orderStreamClient.TradesStream(ctx, &investapi.TradesStreamRequest{})
	if err != nil {
		return fmt.Errorf("trades stream: %w", err)
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				t.logger.Info("Trade stream connection is closed")
				break
			}
			return fmt.Errorf("stream recv: %w", err)
		}

		switch v := resp.Payload.(type) {
		case *investapi.TradesStreamResponse_Ping:
			t.logger.Debug("Trade stream ping was received", zap.Any("ping", v))
		case *investapi.TradesStreamResponse_OrderTrades:
			t.logger.Debug("Order trades were received", zap.Any("orderTrades", v))

			if err := t.processOrderTrades(v.OrderTrades); err != nil {
				return fmt.Errorf("process order trades: %w", err)
			}
		default:
			return errors.New("unexpected payload")
		}
	}
	return nil
}

func (t *Tinkoff) OpenPosition(
	ctx context.Context,
	action trengin.OpenPositionAction,
) (trengin.Position, trengin.PositionClosed, error) {
	if t.currentPosition.Exist() {
		return trengin.Position{}, nil, fmt.Errorf("not support multiple open position")
	}

	ctx = t.ctxWithAuth(ctx)
	openPrice, err := t.openMarketOrder(ctx, action.Type)
	if err != nil {
		return trengin.Position{}, nil, fmt.Errorf("open market order: %w", err)
	}

	var stopLossID string
	if action.StopLossIndent != 0 {
		stopLossID, err = t.setStopLoss(ctx, t.stopLossPriceByOpen(openPrice, action), action.Type)
		if err != nil {
			return trengin.Position{}, nil, fmt.Errorf("set stop order: %w", err)
		}
	}

	if action.TakeProfitIndent != 0 {
		t.logger.Warn("Take profit is not implemented")
	}

	position, err := trengin.NewPosition(action, time.Now(), openPrice.ToFloat())
	if err != nil {
		return trengin.Position{}, nil, fmt.Errorf("new position: %w", err)
	}

	positionClosed := make(chan trengin.Position)
	t.currentPosition.Set(position, stopLossID, "", positionClosed)

	return *position, positionClosed, nil
}

func (t *Tinkoff) ChangeConditionalOrder(
	ctx context.Context,
	action trengin.ChangeConditionalOrderAction,
) (trengin.Position, error) {
	if !t.currentPosition.Exist() {
		return trengin.Position{}, fmt.Errorf("no open position")
	}

	ctx = t.ctxWithAuth(ctx)
	if err := t.cancelStopOrder(ctx); err != nil {
		return trengin.Position{}, err
	}

	if action.StopLoss != 0 {
		stopLossID, err := t.setStopLoss(ctx, t.stopLossPrice(action.StopLoss), t.currentPosition.position.Type)
		if err != nil {
			return trengin.Position{}, err
		}
		t.currentPosition.SetStopLossID(stopLossID)
		t.currentPosition.position.StopLoss = action.StopLoss
	}

	if action.TakeProfit != 0 {
		t.logger.Warn("Take profit is not implemented")
	}

	return *t.currentPosition.Position(), nil
}

func (t *Tinkoff) ClosePosition(ctx context.Context, action trengin.ClosePositionAction) (trengin.Position, error) {
	if !t.currentPosition.Exist() {
		return trengin.Position{}, fmt.Errorf("no open position")
	}

	ctx = t.ctxWithAuth(ctx)
	if err := t.cancelStopOrder(ctx); err != nil {
		return trengin.Position{}, err
	}

	position := t.currentPosition.Position()
	_, err := t.openMarketOrder(ctx, position.Type.Inverse())
	if err != nil {
		return trengin.Position{}, fmt.Errorf("open market order: %w", err)
	}

	return *position, nil
}

func (t *Tinkoff) processOrderTrades(orderTrades *investapi.OrderTrades) error {
	if !t.currentPosition.Exist() {
		return nil
	}
	if orderTrades.AccountId != t.accountID {
		return nil
	}
	if orderTrades.Figi != t.instrumentFIGI {
		return nil
	}
	if orderTrades.Trades[0].Quantity != t.tradedQuantity {
		return nil
	}

	longClosed := t.currentPosition.position.Type.IsLong() &&
		orderTrades.Direction == investapi.OrderDirection_ORDER_DIRECTION_SELL
	shortClosed := t.currentPosition.position.Type.IsShort() &&
		orderTrades.Direction == investapi.OrderDirection_ORDER_DIRECTION_BUY
	if !longClosed && !shortClosed {
		return nil
	}

	var executedQuantity int64
	var closePrice float64
	for _, trade := range orderTrades.GetTrades() {
		executedQuantity += trade.GetQuantity()
		price := NewMoneyValue(trade.Price)
		closePrice += price.ToFloat() * float64(executedQuantity)
	}

	if executedQuantity != t.tradedQuantity {
		t.logger.Warn("Position was closed partially", zap.Int64("executedQuantity", executedQuantity))
		return nil
	}

	closePrice /= float64(executedQuantity)
	err := t.currentPosition.Close(closePrice)
	if err != nil {
		return fmt.Errorf("close: %w", err)
	}

	t.logger.Info(
		"Position was closed",
		zap.Any("orderTrades", orderTrades),
		zap.Float64("closePrice", closePrice),
	)
	return nil
}

func (t *Tinkoff) ctxWithAuth(ctx context.Context) context.Context {
	md := metadata.New(map[string]string{
		"Authorization": "Bearer " + t.token,
	})
	return metadata.NewOutgoingContext(ctx, md)
}

func (t *Tinkoff) openMarketOrder(ctx context.Context, positionType trengin.PositionType) (*MoneyValue, error) {
	direction := investapi.OrderDirection_ORDER_DIRECTION_BUY
	if positionType.IsShort() {
		direction = investapi.OrderDirection_ORDER_DIRECTION_SELL
	}
	orderRequest := &investapi.PostOrderRequest{
		Figi:      t.instrumentFIGI,
		Quantity:  t.tradedQuantity,
		Price:     nil, // todo уточнить, почему невыгодная цена
		Direction: direction,
		AccountId: t.accountID,
		OrderType: investapi.OrderType_ORDER_TYPE_MARKET,
		OrderId:   uuid.New().String(),
	}

	order, err := t.orderClient.PostOrder(ctx, orderRequest)
	if err != nil {
		t.logger.Error("Failed to execute order", zap.Error(err), zap.Any("orderRequest", orderRequest))
		return nil, fmt.Errorf("post order: %w", err)
	}

	if order.ExecutionReportStatus != investapi.OrderExecutionReportStatus_EXECUTION_REPORT_STATUS_FILL {
		t.logger.Error("Order execution status is not fill", zap.Any("orderRequest", orderRequest))
		return nil, errors.New("order execution status is not fill")
	}

	t.logger.Info("Order was executed", zap.Any("orderRequest", orderRequest), zap.Any("order", order))
	return NewMoneyValue(order.ExecutedOrderPrice), nil
}

func (t *Tinkoff) setStopLoss(
	ctx context.Context,
	stopPrice *investapi.Quotation,
	positionType trengin.PositionType,
) (string, error) {
	stopOrderDirection := investapi.StopOrderDirection_STOP_ORDER_DIRECTION_BUY
	if positionType.IsLong() {
		stopOrderDirection = investapi.StopOrderDirection_STOP_ORDER_DIRECTION_SELL
	}
	stopOrderRequest := &investapi.PostStopOrderRequest{
		Figi:           t.instrumentFIGI,
		Quantity:       t.tradedQuantity,
		Price:          nil, // todo поудалять нулевые?
		StopPrice:      stopPrice,
		Direction:      stopOrderDirection,
		AccountId:      t.accountID,
		ExpirationType: investapi.StopOrderExpirationType_STOP_ORDER_EXPIRATION_TYPE_GOOD_TILL_CANCEL,
		StopOrderType:  investapi.StopOrderType_STOP_ORDER_TYPE_STOP_LOSS,
		ExpireDate:     nil,
	}

	stopOrder, err := t.stopOrderClient.PostStopOrder(ctx, stopOrderRequest)
	if err != nil {
		t.logger.Info(
			"Failed to set stop order",
			zap.Any("stopOrderRequest", stopOrderRequest),
			zap.Error(err),
		)
		return "", fmt.Errorf("post stop order: %w", err)
	}

	t.logger.Info(
		"Stop order was set",
		zap.Any("stopOrderRequest", stopOrderRequest),
		zap.Any("stopOrder", stopOrder),
	)
	return stopOrder.StopOrderId, nil
}

func (t *Tinkoff) cancelStopOrder(ctx context.Context) error {
	cancelStopOrderRequest := &investapi.CancelStopOrderRequest{
		AccountId:   t.accountID,
		StopOrderId: t.currentPosition.StopLossID(),
	}
	_, err := t.stopOrderClient.CancelStopOrder(ctx, cancelStopOrderRequest)
	if err != nil {
		t.logger.Error(
			"Failed to cancel stop order",
			zap.Error(err),
			zap.Any("cancelStopOrderRequest", cancelStopOrderRequest),
		)
		return fmt.Errorf("cancel stop order: %w", err)
	}
	return nil
}

func (t *Tinkoff) stopLossPriceByOpen(openPrice *MoneyValue, action trengin.OpenPositionAction) *investapi.Quotation {
	stopLoss := openPrice.ToFloat() - action.StopLossIndent*action.Type.Multiplier()
	return t.stopLossPrice(stopLoss)
}

func (t *Tinkoff) stopLossPrice(stopLoss float64) *investapi.Quotation {
	stopLossUnits, stopLossNano := math.Modf(stopLoss)
	return &investapi.Quotation{
		Units: int64(stopLossUnits),
		Nano:  int32(stopLossNano * 10e8),
	}
}
