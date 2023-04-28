# trengin

[![Lint Status](https://github.com/evsamsonov/trengin/actions/workflows/lint.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=golangci-lint)
[![Test Status](https://github.com/evsamsonov/trengin/actions/workflows/test.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=test)
[![Go Report Card](https://goreportcard.com/badge/github.com/evsamsonov/trengin)](https://goreportcard.com/report/github.com/evsamsonov/trengin)
[![codecov](https://codecov.io/gh/evsamsonov/trengin/branch/master/graph/badge.svg?token=AC751PKE5Y)](https://codecov.io/gh/evsamsonov/trengin)

A golang library to build an automated trading robot. It provides interfaces for implementation of a trading strategy and execution of trading operations.

## Contents 

- [Installing](#installing)
- [How to use](#how-to-use)
- [Main types](#main-types)
- [How to implement Strategy](#how-to-implement-strategy)
- [How to implement Broker](#how-to-implement-broker)
- [Position](#position)
- [Callbacks on events](#callbacks-on-events)
- [Broker implementation](#broker-implementation)
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
ctx := context.Background()
tradingEngine := trengin.New(strategy, broker)
tradingEngine.Run(ctx)
```

## Main types

| Name             | Description                             |
|------------------|-----------------------------------------|
| `Engine`         | Trading engine                          |
| `Strategy`       | Trading strategy                        |
| `Broker`         | Execution of trading operations         |
| `Actions`        | Channel for sending trading actions     |
| `Position`       | Trading position                        |
| `PositionClosed` | Channel for receiving a closed position |


## How to implement a trading strategy

Trading strategy interface. 

```go
type Strategy interface {
	Run(ctx context.Context, actions Actions) error
}
```

The `Run` method implements the logic of trading strategy. 
It can contain analysis of current data, opening and closing positions, tracking current positions, modifying conditional orders.
It must stop on context Done signal.

You can send `OpenPositionAction`, `ClosePositionAction`, `ChangeConditionalOrderAction` in `actions` channel.
Trading engine crashed if get unexpected type.

### OpenPositionAction

Opening a position.

Constructor: `NewOpenPositionAction`

| Name               | Description                           |
|--------------------|---------------------------------------|
| `positionType`     | Position type (long or short)         |
| `stopLossOffset`   | Stop loss offset from opening price   |
| `takeProfitOffset` | Take profit offset from opening price |

### ChangeConditionalOrderAction

Changing a condition order.

Constructor: `NewChangeConditionalOrderAction`

| Name         | Description                                   |
|--------------|-----------------------------------------------|
| `positionID` | UUID                                          |
| `stopLoss`   | New stop loss value (if 0 then leave as is)   |
| `takeProfit` | New take profit value (if 0 then leave as is) |

### ClosePositionAction

Closing a position.

Constructor: `NewClosePositionAction`

| Name         | Description |
|--------------|-------------|
| `positionID` | UUID        |

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

var stopLossIndent, takeProfitIndent float64
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

Broker is defined by the following interface

```go
type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}
```


The `OpenPosition` method should open a new position, return the opened position and a channel 
into which the position will be written when it is closed. 
It should implement tracking of the closure of the position by a conditional order. 
After sending the closed position to the PositionClosed channel, it should be closed.

The `ClosePosition` method should close the position. It should return an instance of the closed position.

The `ChangeConditionalOrder` method should modify the conditional order. It should return the updated position.

## Position

The Position describes a trading position. 
It contains a unique ID, primary and extra data about the position. 
It can be in two states - open or closed. 
Some methods return the correct value only when the position is closed.

The Extra is additional data should only be used for informational purposes and should not be tied 
to the logic of the trading strategy and execution module, except in cases of local use.

It's created through the `NewPosition` constructor. 
The position must be created and closed in the Broker implementation. 
A copy of the position with the ability to set additional data is passed to the Strategy implementation.

**Fields**

| Name         | Description         |
|--------------|---------------------|
| `ID`         | Unique identifier   |
| `Type`       | Position type       |
| `OpenTime`   | Opening time        |
| `OpenPrice`  | Opening price       |
| `CloseTime`  | Closing time        |
| `ClosePrice` | Closing price       |
| `StopLoss`   | Current stop loss   |
| `TakeProfit` | Current take profit |

**Methods**

| Name            | Description                                                                                                                                   |
|-----------------|-----------------------------------------------------------------------------------------------------------------------------------------------|
| `Close`         | Close position. Принимает время закрытия `closeTime` и цену закрытия `closePrice`. Если позиция уже закрыта, вернет ошибку `ErrAlreadyClosed` |
| `Closed`        | Возвращает канал, который будет закрыт при закрытии позиции                                                                                   |
| `IsLong`        | Position type is long                                                                                                                         |
| `IsShort`       | Position type is short                                                                                                                        |
| `Profit`        | Profit by closed position                                                                                                                     |
| `ProfitByPrice` | Profit by passing `price`                                                                                                                     |
| `Duration`      | Position duration from opening time to closing time                                                                                           |
| `Extra`         | Returns extra data by `key` or `nil` if not set                                                                                               |
| `SetExtra`      | Sets `val` for `key`                                                                                                                          |
| `RangeExtra`    | Execute passed function for each extra values                                                                                                 |

## Callbacks on events

Для выполнения дополнительных действий (отправка оповещений, сохранение позиции в БД и т. п.) торговый движок предоставляется 
методы, с помощью которых можно установить колбэки. Методы не потокобезопасны, вызывать следует до запуска стратегии в работу. 

| Method                    | Description                                        |
|---------------------------|----------------------------------------------------|
| OnPositionOpened          | Устанавливает коллбек на открытие позиции          |
| OnConditionalOrderChanged | Устанавливает коллбек на изменение условной заявки |
| OnPositionClosed          | Устанавливает коллбек на закрытие позиции          |

## Broker implementation

| Name                      | Description |
|---------------------------|-------------|
| Tinkoff                   | todo        |

## What's next?

* Implement the initialization of the trading engine with open positions. 



