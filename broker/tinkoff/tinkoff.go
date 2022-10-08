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

	"github.com/google/uuid"
	investapi "github.com/tinkoff/invest-api-go-sdk"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/evsamsonov/trengin"
)

const (
	tinkoffHost             = "invest-public-api.tinkoff.ru:443"
	defaultProtectiveSpread = 5
)

type Tinkoff struct {
	accountID         string
	token             string
	appName           string
	orderClient       investapi.OrdersServiceClient
	stopOrderClient   investapi.StopOrdersServiceClient
	orderStreamClient investapi.OrdersStreamServiceClient
	instrumentFIGI    string
	instrument        *investapi.Instrument
	tradedQuantity    int64
	protectiveSpread  float64
	currentPosition   *currentPosition
	logger            *zap.Logger
}

type Option func(*Tinkoff)

func WithLogger(logger *zap.Logger) Option {
	return func(t *Tinkoff) {
		t.logger = logger
	}
}

func WithAppName(appName string) Option {
	return func(t *Tinkoff) {
		t.appName = appName
	}
}

func WithProtectiveSpread(protectiveSpread float64) Option {
	return func(t *Tinkoff) {
		t.protectiveSpread = protectiveSpread
	}
}

func New(token, accountID, instrumentFIGI string, tradedQuantity int64, opts ...Option) (*Tinkoff, error) {
	conn, err := grpc.Dial(
		tinkoffHost,
		grpc.WithTransportCredentials(
			credentials.NewTLS(&tls.Config{
				InsecureSkipVerify: true, //nolint: gosec
			}),
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
		protectiveSpread:  defaultProtectiveSpread,
		orderClient:       investapi.NewOrdersServiceClient(conn),
		stopOrderClient:   investapi.NewStopOrdersServiceClient(conn),
		orderStreamClient: investapi.NewOrdersStreamServiceClient(conn),
		currentPosition:   &currentPosition{},
		logger:            zap.NewNop(),
	}

	ctx := tinkoff.ctxWithMetadata(context.Background())
	instrumentClient := investapi.NewInstrumentsServiceClient(conn)
	instrumentResponse, err := instrumentClient.GetInstrumentBy(ctx, &investapi.InstrumentRequest{
		IdType: investapi.InstrumentIdType_INSTRUMENT_ID_TYPE_FIGI,
		Id:     instrumentFIGI,
	})
	if err != nil {
		return nil, fmt.Errorf("get instrument by %s: %w", instrumentFIGI, err)
	}
	tinkoff.instrument = instrumentResponse.GetInstrument()

	for _, opt := range opts {
		opt(tinkoff)
	}
	return tinkoff, nil
}

func (t *Tinkoff) Run(ctx context.Context) error {
	ctx = t.ctxWithMetadata(ctx)

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
			if status.Code(err) == codes.Canceled {
				t.logger.Info("Trade stream connection is canceled")
				break
			}
			return fmt.Errorf("stream recv: %w", err)
		}

		switch v := resp.Payload.(type) {
		case *investapi.TradesStreamResponse_Ping:
			t.logger.Debug("Trade stream ping was received", zap.Any("ping", v))
		case *investapi.TradesStreamResponse_OrderTrades:
			t.logger.Info("Order trades were received", zap.Any("orderTrades", v))

			if err := t.processOrderTrades(ctx, v.OrderTrades); err != nil {
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
		return trengin.Position{}, nil, fmt.Errorf("no support multiple open position")
	}

	ctx = t.ctxWithMetadata(ctx)
	openPrice, err := t.openMarketOrder(ctx, action.Type)
	if err != nil {
		return trengin.Position{}, nil, fmt.Errorf("open market order: %w", err)
	}

	var stopLossID, takeProfitID string
	if action.StopLossIndent != 0 {
		stopLossID, err = t.setStopLoss(ctx, t.stopLossPriceByOpen(openPrice, action), action.Type)
		if err != nil {
			return trengin.Position{}, nil, fmt.Errorf("set stop order: %w", err)
		}
	}

	if action.TakeProfitIndent != 0 {
		takeProfitID, err = t.setTakeProfit(ctx, t.takeProfitPriceByOpen(openPrice, action), action.Type)
		if err != nil {
			return trengin.Position{}, nil, fmt.Errorf("set stop order: %w", err)
		}
	}

	position, err := trengin.NewPosition(action, time.Now(), openPrice.ToFloat())
	if err != nil {
		return trengin.Position{}, nil, fmt.Errorf("new position: %w", err)
	}

	positionClosed := make(chan trengin.Position, 1)
	t.currentPosition.Set(position, stopLossID, takeProfitID, positionClosed)

	return *position, positionClosed, nil
}

func (t *Tinkoff) ChangeConditionalOrder(
	ctx context.Context,
	action trengin.ChangeConditionalOrderAction,
) (trengin.Position, error) {
	if !t.currentPosition.Exist() {
		return trengin.Position{}, fmt.Errorf("no open position")
	}

	ctx = t.ctxWithMetadata(ctx)
	if action.StopLoss != 0 {
		if err := t.cancelStopOrder(ctx, t.currentPosition.StopLossID()); err != nil {
			return trengin.Position{}, err
		}

		stopLossID, err := t.setStopLoss(ctx, t.convertFloatToQuotation(action.StopLoss), t.currentPosition.position.Type)
		if err != nil {
			return trengin.Position{}, err
		}
		t.currentPosition.SetStopLossID(stopLossID)
		t.currentPosition.position.StopLoss = action.StopLoss
	}

	if action.TakeProfit != 0 {
		if err := t.cancelStopOrder(ctx, t.currentPosition.TakeProfitID()); err != nil {
			return trengin.Position{}, err
		}

		takeProfitID, err := t.setTakeProfit(ctx, t.convertFloatToQuotation(action.TakeProfit), t.currentPosition.position.Type)
		if err != nil {
			return trengin.Position{}, err
		}
		t.currentPosition.SetTakeProfitID(takeProfitID)
		t.currentPosition.position.TakeProfit = action.TakeProfit
	}

	return *t.currentPosition.Position(), nil
}

func (t *Tinkoff) ClosePosition(ctx context.Context, _ trengin.ClosePositionAction) (trengin.Position, error) {
	if !t.currentPosition.Exist() {
		return trengin.Position{}, fmt.Errorf("no open position")
	}

	ctx = t.ctxWithMetadata(ctx)
	if err := t.cancelStopOrder(ctx, t.currentPosition.StopLossID()); err != nil {
		return trengin.Position{}, fmt.Errorf("cancel stop loss: %w", err)
	}
	if err := t.cancelStopOrder(ctx, t.currentPosition.TakeProfitID()); err != nil {
		return trengin.Position{}, fmt.Errorf("cancel take profit: %w", err)
	}

	position := t.currentPosition.Position()
	logger := t.logger.With(zap.Any("position", position))

	closePrice, err := t.openMarketOrder(ctx, position.Type.Inverse())
	if err != nil {
		return trengin.Position{}, fmt.Errorf("open market order: %w", err)
	}
	if err := t.currentPosition.Close(closePrice.ToFloat()); err != nil {
		if errors.Is(err, trengin.ErrAlreadyClosed) {
			logger.Info("Position already closed")
			return *position, nil
		}
		return trengin.Position{}, fmt.Errorf("close: %w", err)
	}

	logger.Info("Position was closed")
	return *position, nil
}

func (t *Tinkoff) processOrderTrades(ctx context.Context, orderTrades *investapi.OrderTrades) error {
	if !t.currentPosition.Exist() {
		return nil
	}
	if orderTrades.AccountId != t.accountID {
		return nil
	}
	if orderTrades.Figi != t.instrumentFIGI {
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
		closePrice += price.ToFloat() * float64(trade.GetQuantity())
	}

	if executedQuantity != t.tradedQuantity*int64(t.instrument.Lot) {
		t.logger.Warn("Position was closed partially", zap.Int64("executedQuantity", executedQuantity))
		return nil
	}

	err := t.cancelStopOrders(ctx)
	if err != nil {
		return err
	}

	closePrice /= float64(executedQuantity)
	if err := t.currentPosition.Close(closePrice); err != nil {
		if errors.Is(err, trengin.ErrAlreadyClosed) {
			t.logger.Info("Position already closed", zap.Any("position", t.currentPosition))
			return nil
		} else {
			return fmt.Errorf("close: %w", err)
		}
	}

	t.logger.Info(
		"Position was closed by order trades",
		zap.Any("orderTrades", orderTrades),
		zap.Any("position", t.currentPosition),
	)
	return nil
}

func (t *Tinkoff) ctxWithMetadata(ctx context.Context) context.Context {
	md := metadata.New(map[string]string{
		"Authorization": "Bearer " + t.token,
		"x-app-name":    t.appName,
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

type stopOrderType int

const (
	stopLossStopOrderType stopOrderType = iota + 1
	takeProfitStopOrderType
)

func (t *Tinkoff) setStopLoss(
	ctx context.Context,
	price *investapi.Quotation,
	positionType trengin.PositionType,
) (string, error) {
	return t.setStopOrder(ctx, price, positionType, stopLossStopOrderType)
}

func (t *Tinkoff) setTakeProfit(
	ctx context.Context,
	price *investapi.Quotation,
	positionType trengin.PositionType,
) (string, error) {
	return t.setStopOrder(ctx, price, positionType, takeProfitStopOrderType)
}

func (t *Tinkoff) setStopOrder(
	ctx context.Context,
	stopPrice *investapi.Quotation,
	positionType trengin.PositionType,
	orderType stopOrderType,
) (string, error) {
	stopOrderDirection := investapi.StopOrderDirection_STOP_ORDER_DIRECTION_BUY
	if positionType.IsLong() {
		stopOrderDirection = investapi.StopOrderDirection_STOP_ORDER_DIRECTION_SELL
	}
	reqStopOrderType := investapi.StopOrderType_STOP_ORDER_TYPE_STOP_LOSS
	if orderType == takeProfitStopOrderType {
		reqStopOrderType = investapi.StopOrderType_STOP_ORDER_TYPE_TAKE_PROFIT
	}

	price := t.addProtectedSpread(positionType, stopPrice)
	stopOrderRequest := &investapi.PostStopOrderRequest{
		Figi:           t.instrumentFIGI,
		Quantity:       t.tradedQuantity,
		Price:          price,
		StopPrice:      stopPrice,
		Direction:      stopOrderDirection,
		AccountId:      t.accountID,
		ExpirationType: investapi.StopOrderExpirationType_STOP_ORDER_EXPIRATION_TYPE_GOOD_TILL_CANCEL,
		StopOrderType:  reqStopOrderType,
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

func (t *Tinkoff) cancelStopOrder(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	cancelStopOrderRequest := &investapi.CancelStopOrderRequest{
		AccountId:   t.accountID,
		StopOrderId: id,
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

	t.logger.Info(
		"Stop order was canceled",
		zap.String("id", id),
	)
	return nil
}

func (t *Tinkoff) stopLossPriceByOpen(openPrice *MoneyValue, action trengin.OpenPositionAction) *investapi.Quotation {
	stopLoss := openPrice.ToFloat() - action.StopLossIndent*action.Type.Multiplier()
	return t.convertFloatToQuotation(stopLoss)
}

func (t *Tinkoff) takeProfitPriceByOpen(openPrice *MoneyValue, action trengin.OpenPositionAction) *investapi.Quotation {
	takeProfit := openPrice.ToFloat() + action.TakeProfitIndent*action.Type.Multiplier()
	return t.convertFloatToQuotation(takeProfit)
}

func (t *Tinkoff) convertFloatToQuotation(stopLoss float64) *investapi.Quotation {
	stopOrderUnits, stopOrderNano := math.Modf(stopLoss)

	var roundStopOrderNano int32
	if t.instrument.MinPriceIncrement != nil {
		roundStopOrderNano = int32(math.Round(stopOrderNano*10e8/float64(t.instrument.MinPriceIncrement.GetNano()))) *
			t.instrument.MinPriceIncrement.GetNano()
	}
	return &investapi.Quotation{
		Units: int64(stopOrderUnits),
		Nano:  roundStopOrderNano,
	}
}

func (t *Tinkoff) addProtectedSpread(
	positionType trengin.PositionType,
	price *investapi.Quotation,
) *investapi.Quotation {
	priceFloat := NewMoneyValue(price).ToFloat()
	protectiveSpread := priceFloat * t.protectiveSpread / 100
	return t.convertFloatToQuotation(
		priceFloat - positionType.Multiplier()*protectiveSpread,
	)
}

func (t *Tinkoff) cancelStopOrders(ctx context.Context) error {
	ctx = t.ctxWithMetadata(ctx)

	resp, err := t.stopOrderClient.GetStopOrders(ctx, &investapi.GetStopOrdersRequest{
		AccountId: t.accountID,
	})
	if err != nil {
		return err
	}

	orders := make(map[string]struct{})
	for _, order := range resp.StopOrders {
		orders[order.StopOrderId] = struct{}{}
	}

	stopLossID := t.currentPosition.StopLossID()
	if _, ok := orders[stopLossID]; ok {
		if err := t.cancelStopOrder(ctx, stopLossID); err != nil {
			return fmt.Errorf("cancel stop loss: %w", err)
		}
	}
	if _, ok := orders[t.currentPosition.TakeProfitID()]; ok {
		if err := t.cancelStopOrder(ctx, t.currentPosition.TakeProfitID()); err != nil {
			return fmt.Errorf("cancel take profit: %w", err)
		}
	}
	return nil
}
