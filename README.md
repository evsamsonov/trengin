# trengin

_**TR**ading **ENGIN**e_

[![Lint Status](https://github.com/evsamsonov/trengin/actions/workflows/lint.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=golangci-lint)
[![Test Status](https://github.com/evsamsonov/trengin/actions/workflows/test.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=test)
[![Go Report Card](https://goreportcard.com/badge/github.com/evsamsonov/trengin)](https://goreportcard.com/report/github.com/evsamsonov/trengin)
[![codecov](https://codecov.io/gh/evsamsonov/trengin/branch/master/graph/badge.svg?token=AC751PKE5Y)](https://codecov.io/gh/evsamsonov/trengin)

A Go library for building automated trading robots. It separates trading strategy logic from broker interaction.

## Contents

- [Installing](#installing)
- [How to use](#how-to-use)
- [Main types](#main-types)
- [How to implement Strategy](#how-to-implement-strategy)
- [How to implement Broker](#how-to-implement-broker)
- [Position](#position)
- [Callbacks on events](#callbacks-on-events)
- [Broker implementations](#broker-implementations)
- [What's next?](#whats-next)

## Installing

```shell
go get github.com/evsamsonov/trengin/v2
```

## How to use

Import the package.

```go
import "github.com/evsamsonov/trengin/v2"
```

Create an `Engine` instance by passing implementations of `Strategy` and `Broker`, then call `Run`.

```go
tradingEngine := trengin.New(strategy, broker)
tradingEngine.Run(context.TODO())
```

## Main types

| Name             | Description                                                                                  |
|------------------|----------------------------------------------------------------------------------------------|
| `Engine`         | Trading engine                                                                               |
| `Strategy`       | Trading strategy interface                                                                   |
| `Broker`         | Broker interaction interface                                                                 |
| `BrokerRunner`   | Combines `Broker` and `Runner`                                                               |
| `Runner`         | Can be implemented by a `Broker` to start background tasks such as tracking open positions   |
| `Actions`        | Channel for sending trading actions                                                          |
| `Position`       | Trading position                                                                             |
| `PositionClosed` | Channel for receiving a closed position                                                      |
| `PositionID`     | Unique position identifier (UUID)                                                            |

## How to implement Strategy

```go
type Strategy interface {
	Run(ctx context.Context, actions Actions) error
}
```

The `Run` method should implement the trading strategy logic.
It can analyze current data, open and close positions, track open positions, and modify conditional orders.
You can send `OpenPositionAction`, `ClosePositionAction`, and `ChangeConditionalOrderAction` on the `actions` channel.
Closing the `actions` channel stops the engine.

### OpenPositionAction

Opens a trading position.

Constructor: `NewOpenPositionAction`

| Argument           | Description                                                                 |
|--------------------|-----------------------------------------------------------------------------|
| `figi`             | Financial Instrument Global Identifier                                      |
| `positionType`     | Position type (`Long` or `Short`)                                           |
| `quantity`         | Quantity in lots                                                            |
| `stopLossOffset`   | Stop loss offset from the opening price (0 means no stop loss)              |
| `takeProfitOffset` | Take profit offset from the opening price (0 means no take profit)          |

The action struct also has optional fields `SecurityBoard` and `SecurityCode` that can be set when needed.

### ChangeConditionalOrderAction

Changes a conditional order.

Constructor: `NewChangeConditionalOrderAction`

| Argument     | Description                                   |
|--------------|-----------------------------------------------|
| `positionID` | Unique position ID (`PositionID`)             |
| `stopLoss`   | New stop loss value (0 leaves the current value unchanged) |
| `takeProfit` | New take profit value (0 leaves the current value unchanged) |

### ClosePositionAction

Closes a position.

Constructor: `NewClosePositionAction`

| Argument     | Description                       |
|--------------|-----------------------------------|
| `positionID` | Unique position ID (`PositionID`) |

Example of sending an action and receiving the result.

```go
sendActionOrDone := func(ctx context.Context, actions trengin.Actions, action interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case actions <- action:
	}
	return nil
}

var stopLossOffset, takeProfitOffset float64 // Set your values
action := trengin.NewOpenPositionAction("figi", trengin.Long, 1, stopLossOffset, takeProfitOffset)
if err := sendActionOrDone(ctx, actions, action); err != nil {
	// Handle error
}
result, err := action.Result(ctx)
if err != nil {
	// Handle error
}
// result.Position contains the opened position
// result.Closed is a channel that receives the position when it is closed
```

## How to implement Broker

```go
type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}
```

The `OpenPosition` method should open a new position, return the opened position, and provide a `PositionClosed` channel.
It should track when the position is closed by a conditional order.
After sending the closed position to the `PositionClosed` channel, the channel should be closed.

The `ClosePosition` method should close the position and return the closed position.

The `ChangeConditionalOrder` method should modify conditional orders and return the updated position.

You can also implement the `Runner` interface in your broker to start background tasks such as tracking open positions.

```go
type Runner interface {
	Run(ctx context.Context) error
}
```

If the broker implements both `Broker` and `Runner`, the engine starts `Runner.Run` automatically.
Use `trengin.WithPreventBrokerRun(true)` when creating the engine to disable this behavior.

## Position

`Position` describes a trading position.
It contains a unique ID (`PositionID`), primary fields, and extra data.
It can be in one of two states: open or closed.

`Extra` is additional data for local or informational use only.
It should not be part of the trading strategy logic or the broker implementation contract.

Use the `NewPosition` constructor to create a position in the broker implementation:

```go
position, err := trengin.NewPosition(action, openTime, openPrice)
```

The position must be created and closed in the broker implementation.

**Fields**

| Name             | Description                            |
|------------------|----------------------------------------|
| `ID`             | Unique identifier (`PositionID`)       |
| `SecurityBoard`  | Trading mode identifier (e.g. `TQBR`)  |
| `SecurityCode`   | Security ticker (e.g. `SBER`)          |
| `FIGI`           | Financial Instrument Global Identifier |
| `Quantity`       | Quantity in lots                       |
| `Type`           | Type (`Long` or `Short`)               |
| `OpenTime`       | Opening time                           |
| `OpenPrice`      | Opening price                          |
| `CloseTime`      | Closing time                           |
| `ClosePrice`     | Closing price                          |
| `StopLoss`       | Current stop loss                      |
| `TakeProfit`     | Current take profit                    |
| `Commission`     | Commission                             |

**Methods**

| Name             | Description                                                                                  |
|------------------|----------------------------------------------------------------------------------------------|
| `Close`          | Closes the position. Returns `ErrAlreadyClosed` if the position is already closed           |
| `Closed`         | Returns a channel that is closed when the position is closed                                 |
| `IsClosed`       | Returns whether the position is closed                                                       |
| `IsLong`         | Returns whether the position type is long                                                      |
| `IsShort`        | Returns whether the position type is short                                                     |
| `AddCommission`  | Adds commission to the position                                                              |
| `Profit`         | Profit of a closed position                                                                  |
| `UnitProfit`     | Profit per lot for a closed position                                                         |
| `UnitCommission` | Commission per lot                                                                           |
| `ProfitByPrice`  | Profit at the given price (useful for open positions)                                        |
| `Duration`       | Position duration from opening time to closing time                                          |
| `Extra`          | Returns extra data by `key`, or `nil` if not set                                               |
| `SetExtra`       | Sets `val` for `key`                                                                         |
| `RangeExtra`     | Executes the passed function for each extra value                                              |

## Callbacks on events

To perform additional actions (sending notifications, saving a position to a database, etc.),
the trading engine provides callback methods.
These methods are not thread-safe and must be set before running the engine.

| Method                    | Description                                      |
|---------------------------|--------------------------------------------------|
| `OnPositionOpened`        | Sets a callback for position opening             |
| `OnConditionalOrderChanged` | Sets a callback for conditional order changes  |
| `OnPositionClosed`        | Sets a callback for position closing             |

## Broker implementations

| Name                                                                      | Description                                                     |
|---------------------------------------------------------------------------|-----------------------------------------------------------------|
| [evsamsonov/tinkoff-broker](https://github.com/evsamsonov/tinkoff-broker) | Uses [Tinkoff Invest API](https://tinkoff.github.io/investAPI/) |
| [evsamsonov/finam-broker](https://github.com/evsamsonov/finam-broker)     | Uses [Finam Trade API](https://finamweb.github.io/trade-api-docs/) |

## What's next?

* Implement engine initialization with already open positions.
