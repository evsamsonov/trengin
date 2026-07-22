package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/evsamsonov/trengin/v2"
)

func main() {
	ctx := context.Background()
	engine := trengin.New(demoStrategy{}, newMemoryBroker(100, 108))

	engine.
		OnPositionOpened(func(position trengin.Position) {
			fmt.Printf(
				"opened: figi=%s price=%.2f stop=%.2f take=%.2f\n",
				position.FIGI,
				position.OpenPrice,
				position.StopLoss,
				position.TakeProfit,
			)
		}).
		OnConditionalOrderChanged(func(position trengin.Position) {
			fmt.Printf(
				"changed: figi=%s stop=%.2f take=%.2f\n",
				position.FIGI,
				position.StopLoss,
				position.TakeProfit,
			)
		}).
		OnPositionClosed(func(position trengin.Position) {
			fmt.Printf(
				"closed: figi=%s price=%.2f profit=%.2f\n",
				position.FIGI,
				position.ClosePrice,
				position.Profit(),
			)
		})

	if err := engine.Run(ctx); err != nil {
		log.Printf("Failed to run trading engine: %v", err)
		os.Exit(1)
	}
}
