package tinkoff

import (
	"context"
	"log"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/evsamsonov/trengin"
)

func TestNew(t *testing.T) {
	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatal(err)
		return
	}

	return

	tinkoffBroker, err := New(
		"t.VDdl3jc91JLcHlPDmXtZTulMdkHQeAEwuWQnBPNuWqc0zGlzWBcntK-jd6rtDYBBroV5ixDNd2fQYCnGfFiqkQ",
		"2014657312",
		"BBG004730N88", // FUTSBRF06220  BBG004730N88
		1,
		WithLogger(logger),
	)
	if err != nil {
		log.Fatalf("Failed to create Tinkoff Broker: %s", err)
		return
	}

	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)

	// todo решить проблему отмены контекста
	// проверить ошибку на context.Canceled
	// todo пересмотреть еще раз
	// todo начать писать стратегию

	go func() {
		if err := tinkoffBroker.Run(ctx); err != nil {
			log.Fatalf("Failed to run Tinkoff Broker: %s", err)
			return
		}
		logger.Info("Tinkoff broker was stopped")
	}()

	position, positionClosed, err := tinkoffBroker.OpenPosition(ctx, trengin.OpenPositionAction{
		Type:             trengin.Long,
		StopLossIndent:   170,
		TakeProfitIndent: 0,
	})
	if err != nil {
		log.Fatalf("Failed to open position: %s", err)
		return
	}
	logger.Info("Position was opened", zap.Any("position", position))

	go func() {
		pos := <-positionClosed
		logger.Info("Position closed was received", zap.Any("position", pos))
	}()

	time.Sleep(1 * time.Minute)

	//position, err = tinkoffBroker.ChangeConditionalOrder(ctx, trengin.ChangeConditionalOrderAction{
	//	PositionID: position.ID,
	//	StopLoss:   position.StopLoss + 70,
	//	TakeProfit: 0,
	//})
	//if err != nil {
	//	log.Fatalf("Failed to change conditional order: %s", err)
	//	return
	//}
	//logger.Info("Conditional order was changed", zap.Any("position", position))
	//
	//time.Sleep(1 * time.Minute)

	position, err = tinkoffBroker.ChangeConditionalOrder(ctx, trengin.ChangeConditionalOrderAction{
		PositionID: position.ID,
		StopLoss:   position.OpenPrice,
		TakeProfit: 0,
	})
	if err != nil {
		log.Fatalf("Failed to change consditional order: %s", err)
		return
	}
	logger.Info("Conditional order was changed", zap.Any("position", position))

	time.Sleep(1 * time.Minute)

	position, err = tinkoffBroker.ClosePosition(ctx, trengin.ClosePositionAction{
		PositionID: position.ID,
	})
	if err != nil {
		log.Fatalf("Failed to close position: %s", err)
		return
	}
	logger.Info("Position was closed", zap.Any("position", position))

	cancel()

	// todo что будет, если стоп заявка уже отменена?
	// todo как обработать отмену контекста?
}
