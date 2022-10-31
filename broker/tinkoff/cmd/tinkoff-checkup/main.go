package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"

	"go.uber.org/zap"
	"golang.org/x/term"

	"github.com/evsamsonov/trengin"
	"github.com/evsamsonov/trengin/broker/tinkoff"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println(
			"This command tests methods Tinkoff Broker implementations.\n" +
				"It opens position, changes conditional order, closes position.",
		)
		fmt.Println("\nUsage: tinkoff-checkup [ACCOUNT_ID] [INSTRUMENT_FIGI]")
		return
	}
	accountID := os.Args[1]
	instrumentFIGI := os.Args[2]

	logger, err := zap.NewDevelopment(zap.IncreaseLevel(zap.DebugLevel))
	if err != nil {
		log.Fatalf("Failed to create logger: %s", err)
		return
	}

	fmt.Printf("Paste Tinkoff token: ")
	tokenBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		logger.Fatal("Failed to read token", zap.Error(err))
		return
	}
	fmt.Println("")

	tinkoffBroker, err := tinkoff.New(
		string(tokenBytes),
		accountID,
		instrumentFIGI,
		tinkoff.WithLogger(logger),
	)
	if err != nil {
		logger.Fatal("Failed to create Tinkoff Broker", zap.Error(err))
		return
	}

	var stopLossIndent, takeProfitIndent float64
	fmt.Print("Enter stop loss indent: ")
	if _, err = fmt.Scanln(&stopLossIndent); err != nil {
		logger.Fatal("Failed to read stop loss indent", zap.Error(err))
		return
	}
	fmt.Print("Enter take profit indent: ")
	if _, err = fmt.Scanln(&takeProfitIndent); err != nil {
		logger.Fatal("Failed to read stop loss indent: %s", zap.Error(err))
		return
	}

	var wg sync.WaitGroup
	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("Tinkoff broker is running")
		if err := tinkoffBroker.Run(ctx); err != nil {
			logger.Fatal("Failed to run Tinkoff Broker", zap.Error(err))
			return
		}
		cancel()
		logger.Info("Tinkoff broker was stopped")
	}()

	fmt.Println("Press any key for open position")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')

	openPositionAction := trengin.OpenPositionAction{
		Type:             trengin.Long,
		StopLossIndent:   stopLossIndent,
		TakeProfitIndent: takeProfitIndent,
	}
	position, positionClosed, err := tinkoffBroker.OpenPosition(ctx, openPositionAction)
	if err != nil {
		logger.Fatal("Failed to open position", zap.Error(err), zap.Any("action", openPositionAction))
		return
	}
	logger.Info("Position was opened", zap.Any("position", position))

	wg.Add(1)
	go func() {
		defer wg.Done()
		// todo добавить контекст
		pos := <-positionClosed
		logger.Info("Closed position was received", zap.Any("position", pos))
	}()

	fmt.Println("Press any key for reduce by half conditional orders")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')

	changeConditionalOrderAction := trengin.ChangeConditionalOrderAction{
		PositionID: position.ID,
		StopLoss:   position.OpenPrice - stopLossIndent/2,
		TakeProfit: position.OpenPrice + takeProfitIndent/2,
	}
	position, err = tinkoffBroker.ChangeConditionalOrder(ctx, changeConditionalOrderAction)
	if err != nil {
		logger.Fatal(
			"Failed to change conditional order: %s",
			zap.Error(err),
			zap.Any("action", changeConditionalOrderAction),
		)
		return
	}
	logger.Info("Conditional order was changed", zap.Any("position", position))

	fmt.Println("Press any key for close position")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')

	closePositionAction := trengin.ClosePositionAction{
		PositionID: position.ID,
	}
	position, err = tinkoffBroker.ClosePosition(ctx, closePositionAction)
	if err != nil {
		logger.Fatal("Failed to close position", zap.Error(err), zap.Any("action", closePositionAction))
		return
	}
	logger.Info("Position was closed", zap.Any("position", position))

	cancel()
	wg.Wait()
	logger.Info("Check up is successful! 🍺")
}
