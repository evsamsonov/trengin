# trengin 

_**TR**ading **ENGIN**e_

[![Lint Status](https://github.com/evsamsonov/trengin/actions/workflows/lint.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=golangci-lint)
[![Test Status](https://github.com/evsamsonov/trengin/actions/workflows/test.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=test)
[![Go Report Card](https://goreportcard.com/badge/github.com/evsamsonov/trengin)](https://goreportcard.com/report/github.com/evsamsonov/trengin)
[![codecov](https://codecov.io/gh/evsamsonov/trengin/branch/master/graph/badge.svg?token=AC751PKE5Y)](https://codecov.io/gh/evsamsonov/trengin)

A golang library to build an automated trading robot. It provides the ability to separate trading strategy logic and interaction with broker.


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
go get github.com/evsamsonov/trengin
```

## How to use

Import the package.

```go
import "github.com/evsamsonov/trengin"
```

Create an Engine instance passing implementations of Strategy and Broker and call Run.

```go
tradingEngine := trengin.New(strategy, broker)
tradingEngine.Run(context.TODO())
```

## Main types

| Name             | Description                                                                                |
|------------------|--------------------------------------------------------------------------------------------|
| `Engine`         | Trading engine                                                                             |
| `Strategy`       | Interface of trading strategy                                                              |
| `Broker`         | Interface of interaction with broker                                                       |
| `Runner`         | Сan be implemented Broker client to starts background tasks such as tracking open position |
| `Actions`        | Channel for sending trading actions                                                        |
| `Position`       | Trading position                                                                           |
| `PositionClosed` | Channel for receiving a closed position                                                    |

## How to implement Strategy

```go
type Strategy interface {
	Run(ctx context.Context, actions Actions) error
}
```

The `Run` method should implement trading strategy logic. 
It can contain analysis of current data, opening and closing positions, tracking current positions, modifying conditional orders.
You can send `OpenPositionAction`, `ClosePositionAction`, `ChangeConditionalOrderAction` in `actions` channel.

### OpenPositionAction

Opening a trading position.

Constructor: `NewOpenPositionAction`

| Arguments          | Description                           |
|--------------------|---------------------------------------|
| `positionType`     | Position type (long or short)         |
| `stopLossOffset`   | Stop loss offset from opening price   |
| `takeProfitOffset` | Take profit offset from opening price |

### ChangeConditionalOrderAction

Changing a condition order.

Constructor: `NewChangeConditionalOrderAction`

| Name         | Description                                   |
|--------------|-----------------------------------------------|
| `positionID` | Unique ID (UUID)                              |
| `stopLoss`   | New stop loss value (if 0 then leave as is)   |
| `takeProfit` | New take profit value (if 0 then leave as is) |

### ClosePositionAction

Closing a position.

Constructor: `NewClosePositionAction`

| Name         | Description       |
|--------------|-------------------|
| `positionID` | Unique ID  (UUID) |

An example of sending an action and receiving the result. 

```go
sendActionOrDone := func(ctx context.Context, action interface{}) error {
    select {
    case <-ctx.Done():
    	return ctx.Err()
    case s.actions <- action:
    }
    return nil
}

var stopLossIndent, takeProfitIndent float64 // Set your values
action := trengin.NewOpenPositionAction(trengin.Long, stopLossOffset, takeProfitOffset)
if err = s.sendActionOrDone(ctx, action); err != nil {
    // Handle error
}
result, err := action.Result(ctx)
if err != nil {
    // Handle error
}
```

## How to implement Broker

```go
type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}
```

The `OpenPosition` method should open a new position, return the opened position and a `PositionClosed` channel.
It should implement tracking of the closure of the position by a conditional order. 
After sending the closed position to the `PositionClosed`, it should be closed.

The `ClosePosition` method should close the position. It should return the closed position.

The `ChangeConditionalOrder` method should modify the conditional orders. It should return the updated position.

## Position

The Position describes a trading position. 
It contains a unique ID (UUID), primary and extra data. 
It can be in two states &mdash; open or closed.

The `Extra` is additional data should only be used for informational purposes and should not be tied 
to the trading strategy logic and the Broker implementation, except in cases of local use.

Use `NewPosition` constructor to create Position.  
The position must be created and closed in the Broker implementation.

**Fields**

| Name         | Description                            |
|--------------|----------------------------------------|
| `ID`         | Unique identifier (UUID)               |
| `FIGI`       | Financial Instrument Global Identifier |
| `Quantity`   | Quantity in lots                       |
| `Type`       | Type (long or short)                   |
| `OpenTime`   | Opening time                           |
| `OpenPrice`  | Opening price                          |
| `CloseTime`  | Closing time                           |
| `ClosePrice` | Closing price                          |
| `StopLoss`   | Current stop loss                      |
| `TakeProfit` | Current take profit                    |
| `Commission` | Commission                             |

**Methods**

| Name             | Description                                                                                  |
|------------------|----------------------------------------------------------------------------------------------|
| `Close`          | Close position. If the position is already closed it will return an `ErrAlreadyClosed` error |
| `Closed`         | Returns a channel that will be closed upon closing the position                              |
| `IsClosed`       | Position is closed                                                                           |
| `IsLong`         | Position type is long                                                                        |
| `IsShort`        | Position type is short                                                                       |
| `AddCommission`  | Position type is short                                                                       |
| `Profit`         | Profit by closed position                                                                    |
| `UnitProfit`     | Profit on a lot by closed position                                                           |
| `UnitCommission` | Commission on a lot by closed position                                                       |
| `ProfitByPrice`  | Profit by passing `price`                                                                    |
| `Duration`       | Position duration from opening time to closing time                                          |
| `Extra`          | Returns extra data by `key` or `nil` if not set                                              |
| `SetExtra`       | Sets `val` for `key`                                                                         |
| `RangeExtra`     | Executes passed function for each extra values                                               |

## Callbacks on events

To perform additional actions (sending notifications, saving position in the database, etc.), 
the trading engine provides methods to set callbacks. 
The methods are not thread-safe and should be called before running the strategy.

| Method                    | Description                                        |
|---------------------------|----------------------------------------------------|
| OnPositionOpened          | Sets callback on opening position                  |
| OnConditionalOrderChanged | Sets callback on changing condition order position |
| OnPositionClosed          | Sets callback on closing position                  |

## Broker implementations

| Name                                                                      | Description                                                     |
|---------------------------------------------------------------------------|-----------------------------------------------------------------|
| [evsamsonov/tinkoff-broker](https://github.com/evsamsonov/tinkoff-broker) | It uses Tinkoff Invest API https://tinkoff.github.io/investAPI/ |

## What's next?

* Implement the initialization of the trading engine with open positions. 



