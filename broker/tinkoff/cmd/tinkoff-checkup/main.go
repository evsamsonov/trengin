package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"syscall"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
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
		fmt.Println("\nUsage: tinkoff-checkup [ACCOUNT_ID] [INSTRUMENT_FIGI] [-v]")
		return
	}
	accountID := os.Args[1]
	instrumentFIGI := os.Args[2]

	verbose := flag.Bool("v", false, "")
	if err := flag.CommandLine.Parse(os.Args[3:]); err != nil {
		log.Fatalf("Failed to parse args: %s", err)
	}

	checkupParams := NewCheckupParams(accountID, instrumentFIGI)
	if err := checkupParams.AskUser(); err != nil {
		log.Fatalf("Failed to get checkup params: %s", err)
	}

	checkuper, err := NewTinkoffCheckuper(*verbose)
	if err != nil {
		log.Fatalf("Failed to create tinkoff checkuper: %s", err)
	}
	if err := checkuper.CheckUp(checkupParams); err != nil {
		log.Fatalf("Failed to check up: %s", err)
	}
	fmt.Println("Check up is successful! 🍺")
}

type CheckUpArgs struct {
	accountID        string
	instrumentFIGI   string
	tinkoffToken     string
	stopLossIndent   float64
	takeProfitIndent float64
	positionType     trengin.PositionType
}

func NewCheckupParams(accountID, instrumentFIGI string) CheckUpArgs {
	return CheckUpArgs{
		accountID:      accountID,
		instrumentFIGI: instrumentFIGI,
	}
}

func (c *CheckUpArgs) AskUser() error {
	fmt.Printf("Paste Tinkoff token: ")
	tokenBytes, err := term.ReadPassword(syscall.Stdin)
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	fmt.Println()
	c.tinkoffToken = string(tokenBytes)

	var positionType string
	fmt.Print("Enter position direction [long, short]: ")
	if _, err = fmt.Scanln(&positionType); err != nil {
		return fmt.Errorf("read stop loss indent: %w", err)
	}
	if positionType != "long" && positionType != "short" {
		return fmt.Errorf("read position direction: %w", err)
	}
	c.positionType = trengin.Long
	if positionType == "short" {
		c.positionType = trengin.Short
	}

	var stopLossIndent, takeProfitIndent float64
	fmt.Print("Enter stop loss indent [0 - skip]: ")
	if _, err = fmt.Scanln(&stopLossIndent); err != nil {
		return fmt.Errorf("read stop loss indent: %w", err)
	}
	c.stopLossIndent = stopLossIndent

	fmt.Print("Enter take profit indent [0 - skip]: ")
	if _, err = fmt.Scanln(&takeProfitIndent); err != nil {
		return fmt.Errorf("read take profit indent: %w", err)
	}
	c.takeProfitIndent = takeProfitIndent
	return nil
}

type TinkoffCheckuper struct {
	logger *zap.Logger
}

func NewTinkoffCheckuper(verbose bool) (*TinkoffCheckuper, error) {
	logger := zap.NewNop()
	if verbose {
		var err error
		logger, err = zap.NewDevelopment(zap.IncreaseLevel(zap.DebugLevel))
		if err != nil {
			return nil, fmt.Errorf("create logger: %w", err)
		}
	}
	return &TinkoffCheckuper{
		logger: logger,
	}, nil
}

func (t *TinkoffCheckuper) CheckUp(params CheckUpArgs) error {
	tinkoffBroker, err := tinkoff.New(
		params.tinkoffToken,
		params.accountID,
		params.instrumentFIGI,
		tinkoff.WithLogger(t.logger),
	)
	if err != nil {
		return fmt.Errorf("create tinkoff broker: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		defer cancel()
		if err := tinkoffBroker.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("tinkoff broker: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		defer cancel()
		t.WaitAnyKey("Press any key for open position...")

		openPositionAction := trengin.OpenPositionAction{
			Type:             params.positionType,
			Quantity:         1,
			StopLossIndent:   params.stopLossIndent,
			TakeProfitIndent: params.takeProfitIndent,
		}
		position, positionClosed, err := tinkoffBroker.OpenPosition(ctx, openPositionAction)
		if err != nil {
			return fmt.Errorf("open position: %w", err)
		}
		fmt.Printf(
			"Position opened. Open price: %f, stop loss: %f, take profit: %f\n",
			position.OpenPrice,
			position.StopLoss,
			position.TakeProfit,
		)

		g.Go(func() error {
			select {
			case <-ctx.Done():
				return nil
			case pos := <-positionClosed:
				fmt.Printf(
					"Position closed. Conditional orders removed. Close price: %f, profit: %f\n",
					pos.ClosePrice,
					pos.Profit(),
				)
			}
			return nil
		})
		t.WaitAnyKey("Press any key for reduce by half conditional orders...")

		changeConditionalOrderAction := trengin.ChangeConditionalOrderAction{
			PositionID: position.ID,
			StopLoss:   position.OpenPrice - params.stopLossIndent/2,
			TakeProfit: position.OpenPrice + params.takeProfitIndent/2,
		}
		position, err = tinkoffBroker.ChangeConditionalOrder(ctx, changeConditionalOrderAction)
		if err != nil {
			return fmt.Errorf("change condition order: %w", err)
		}
		fmt.Printf(
			"Conditional orders changed. New stop loss: %f, new take profit: %f\n",
			position.StopLoss,
			position.TakeProfit,
		)
		t.WaitAnyKey("Press any key for close position...")

		closePositionAction := trengin.ClosePositionAction{PositionID: position.ID}
		position, err = tinkoffBroker.ClosePosition(ctx, closePositionAction)
		if err != nil {
			return fmt.Errorf("close position: %w", err)
		}
		return nil
	})

	return g.Wait()
}
func (t *TinkoffCheckuper) WaitAnyKey(msg string) {
	fmt.Print(msg)
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
