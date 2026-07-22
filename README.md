# trengin

_**TR**ading **ENGIN**e_

[![Lint Status](https://github.com/evsamsonov/trengin/actions/workflows/lint.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions/workflows/lint.yml)
[![Test Status](https://github.com/evsamsonov/trengin/actions/workflows/test.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/evsamsonov/trengin/v2.svg)](https://pkg.go.dev/github.com/evsamsonov/trengin/v2)
[![Release](https://img.shields.io/github/v/release/evsamsonov/trengin)](https://github.com/evsamsonov/trengin/releases)
[![codecov](https://codecov.io/gh/evsamsonov/trengin/branch/master/graph/badge.svg?token=AC751PKE5Y)](https://codecov.io/gh/evsamsonov/trengin)

A Go library for building automated trading robots.
It separates trading strategy logic from broker interaction:

```text
Strategy  -->  Engine  -->  Broker
   |              |            |
 actions     callbacks     positions
```

## Contents

- [Installing](#installing)
- [Quick start](#quick-start)
- [Main types](#main-types)
- [How to implement Strategy](#how-to-implement-strategy)
- [How to implement Broker](#how-to-implement-broker)
- [Position](#position)
- [Callbacks on events](#callbacks-on-events)
- [Broker implementations](#broker-implementations)
- [What's next?](#whats-next)

## Installing

```shell
go get github.com/evsamsonov/trengin/v2@latest
```

## Quick start

Create an `Engine` with your `Strategy` and `Broker` implementations, then call `Run`:

```go
import (
	"context"
	"log"

	"github.com/evsamsonov/trengin/v2"
)

func main() {
	engine := trengin.New(strategy, broker)
	if err := engine.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
```

For a complete runnable example with an in-memory broker, see
[examples/simple](examples/simple):

```shell
go run ./examples/simple
```

The example shows how a strategy sends actions, waits for results, and how
engine callbacks report opened, changed, and closed positions.

## Main types

| Name             | Description                                                                                |
|------------------|--------------------------------------------------------------------------------------------|
| `Engine`         | Trading engine that connects `Strategy` and `Broker`                                       |
| `Strategy`       | Trading strategy interface                                                                 |
| `Broker`         | Broker interaction interface                                                               |
| `Runner`         | Optional broker interface for background tasks such as tracking open positions             |
| `BrokerRunner`   | Combines `Broker` and `Runner`                                                             |
| `Actions`        | Channel for sending trading actions from strategy to engine                                |
| `Position`       | Trading position                                                                           |
| `PositionClosed` | Channel that receives a position when it is closed                                         |
| `PositionID`     | Unique position identifier (`UUID`)                                                        |

Full API reference: [pkg.go.dev/github.com/evsamsonov/trengin/v2](https://pkg.go.dev/github.com/evsamsonov/trengin/v2).

## How to implement Strategy

```go
type Strategy interface {
	Run(ctx context.Context, actions Actions) error
}
```

`Run` contains strategy logic: analyze data, open or close positions, and modify
conditional orders. Send actions on the `actions` channel:

- `OpenPositionAction`
- `ChangeConditionalOrderAction`
- `ClosePositionAction`

Closing the `actions` channel stops the engine.

### OpenPositionAction

Constructor: `NewOpenPositionAction(figi, positionType, quantity, stopLossOffset, takeProfitOffset)`

| Argument           | Description                                                        |
|--------------------|--------------------------------------------------------------------|
| `figi`             | Financial Instrument Global Identifier                             |
| `positionType`     | `Long` or `Short`                                                  |
| `quantity`         | Quantity in lots                                                   |
| `stopLossOffset`   | Stop loss offset from the opening price (`0` means no stop loss)   |
| `takeProfitOffset` | Take profit offset from the opening price (`0` means no take profit) |

Optional fields `SecurityBoard` and `SecurityCode` can be set on the action when needed.

### ChangeConditionalOrderAction

Constructor: `NewChangeConditionalOrderAction(positionID, stopLoss, takeProfit)`

Pass `0` for `stopLoss` or `takeProfit` to leave the current value unchanged.

### ClosePositionAction

Constructor: `NewClosePositionAction(positionID)`

### Sending an action

```go
action := trengin.NewOpenPositionAction("figi", trengin.Long, 1, stopLossOffset, takeProfitOffset)
select {
case <-ctx.Done():
	return ctx.Err()
case actions <- action:
}

result, err := action.Result(ctx)
if err != nil {
	return err
}
// result.Position — opened position
// result.Closed   — receives the position when it is closed
```

See also the full flow in [examples/simple](examples/simple).

## How to implement Broker

```go
type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}
```

- `OpenPosition` opens a position, returns it, and provides a `PositionClosed` channel.
  Track closure by conditional orders; send the closed position to the channel, then close the channel.
- `ClosePosition` closes a position and returns it.
- `ChangeConditionalOrder` updates stop loss / take profit and returns the updated position.

Create positions with `trengin.NewPosition` inside the broker implementation.

You can also implement `Runner` to start background tasks:

```go
type Runner interface {
	Run(ctx context.Context) error
}
```

If the broker implements both `Broker` and `Runner`, the engine starts `Runner.Run`
automatically. Disable this with `trengin.WithPreventBrokerRun(true)`.

## Position

A position has unique `ID`, market fields, and optional `Extra` data.
It is either open or closed.

`Extra` is for local or informational use only.
Do not rely on it in strategy or broker contract logic.

Main fields: `ID`, `SecurityBoard`, `SecurityCode`, `FIGI`, `Type`, `Quantity`,
`OpenTime`, `OpenPrice`, `CloseTime`, `ClosePrice`, `StopLoss`, `TakeProfit`, `Commission`.

Useful methods: `Close`, `Closed`, `IsClosed`, `IsLong`, `IsShort`, `Profit`,
`UnitProfit`, `ProfitByPrice`, `Duration`, `Extra` / `SetExtra` / `RangeExtra`.

## Callbacks on events

Set callbacks before `Run`. These methods are not thread-safe.

| Method                      | Description                            |
|-----------------------------|----------------------------------------|
| `OnPositionOpened`          | Called when a position is opened       |
| `OnConditionalOrderChanged` | Called when stop loss / take profit changes |
| `OnPositionClosed`          | Called when a position is closed       |

Typical uses: notifications, persistence, metrics.

## Broker implementations

| Name                                                                      | Description |
|---------------------------------------------------------------------------|-------------|
| [evsamsonov/tinkoff-broker](https://github.com/evsamsonov/tinkoff-broker) | [Tinkoff Invest API](https://russianinvestments.github.io/investAPI/) |
| [evsamsonov/finam-broker](https://github.com/evsamsonov/finam-broker)     | [Finam Trade API](https://api.finam.ru/docs/rest/) |

## What's next?

* Initialize the engine with already open positions.
