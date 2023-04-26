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

В методе `Run` реализуется логика торговой стратегии. Здесь может быть анализ текущих данных, открытие позиции, 
анализ данных по открытой позиции для закрытия или для изменения условной заявки (перестановка стопа в безубыток, трейлинг стоп и т. п.).

Method `Run` allow to implement trading strategy logic. It may contain current data analysis, open or close position, track current positions, 
change conditional order (break-even, trailing stop, etc).

Должно останаливаться на ctx.Done 

You can send `OpenPositionAction`, `ClosePositionAction`, `ChangeConditionalOrderAction` in `actions` channel. 
Use constructor to create it. 
Trading engine crashed if you send unexpected type. 


В канал торговых действий можно отправить экземпляры `OpenPositionAction`, `ClosePositionAction`, `ChangeConditionalOrderAction`. 
Создаются через конструкторы. При отправке неожидаемых типов торговый движок завершит работу с ошибкой.

### OpenPositionAction

Opening a position.

Constructor: `NewOpenPositionAction`

| Name               | Description                                  |
|--------------------|----------------------------------------------|
| `positionType`     | Тип позиции (покупка или продажа)            |
| `stopLossIndent`   | Отступ стоп-лосса от цены открытия позиции   |
| `takeProfitIndent` | Отступ тейк-профита от цены открытия позиции |

### ChangeConditionalOrderAction

Changing a condition order.

Constructor: `NewChangeConditionalOrderAction`

| Name         | Description                                                  |
|--------------|--------------------------------------------------------------|
| `positionID` | Идентификатор позиции                                        |
| `stopLoss`   | Новое значения для стоп-лосса (если равно 0, то не изменять) |
| `takeProfit` | Новое значения для стоп-лосса (если равно 0, то не изменять) |

### ClosePositionAction

Closing a position.

Constructor: `NewClosePositionAction`

| Name         | Description           |
|--------------|-----------------------|
| `positionID` | Идентификатор позиции |

Пример отправки действия и получения результата.

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
action := trengin.NewOpenPositionAction(trengin.Long, stopLossIndent, takeProfitIndent)
if err = s.sendActionOrDone(ctx, action); err != nil {
    // Обработка ошибки
}
result, err := action.Result(ctx)
if err != nil {
    // Обработка ошибки
}
```

## Как реализовать модуль исполнения торговых операций

Описан следующим интерфейсом.

```go
type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}
```

Метод `OpenPosition` должен открывать новую позицию по `action`, возвращать экземпляр открытой позиции и канал, в который будет записана позиция при её закрытии. Должен реализовывать отслеживания закрытия позиции по условной заявке. После отправки закрытой позиции канал `PositionClosed` требуется закрыть.

Метод `ClosePosition` должен закрывать позицию по `action`. Возвращать экземпляр закрытой позиции. 

Метод `ChangeConditionalOrder` должен изменить условную заявку по `action`. Возвращать актуальный экземпляр позиции.

## Position

Структура `Position` описывает торговую позицию. Содержит уникальный в рамках одного запуска идентификатор `ID` , основные и дополнительные данные о позиции. Может быть в двух состояниях — открытом и закрытом. Некоторые методы возвращают корректное значение только при закрытой позиции. 

Дополнительные данные `Extra` следует использовать только в информационных целях, не завязывая на них логику работы стратегии и модуля исполнения торговых операций. Кроме случаев локального использования.  

Создается через конструктор `NewPosition` по `action` с временем открытия `openTime` и ценой открытия `openPrice`. Позиция должна создаваться и закрываться в реализации `Broker`. В реализацию `Strategy` передается копия позиции с возможностью установить дополнительные данные.

**Fields**

| Name | Description |
| ------------- | ------------- |
| `ID` | Уникальный идентификатор в рамках запуска |
| `Type` | Тип |
| `OpenTime` | Время открытия |
| `OpenPrice` | Цена открытия |
| `CloseTime` | Время закрытия |
| `ClosePrice` | Цена закрытия |
| `StopLoss` | Текущий стоп лосс |
| `TakeProfit` | Текущий тейк профит |

**Methods**

|  Name  | Description | 
| ------------- | ------------- | 
| `Close` | Метод для закрытия позиции. Принимает время закрытия `closeTime` и цену закрытия `closePrice`. Если позиция уже закрыта, вернет ошибку `ErrAlreadyClosed` |
| `Closed` | Возвращает канал, который будет закрыт при закрытии позиции |
| `IsLong` | Тип сделки "покупка" |
| `IsShort` | Тип сделки "продажа" |
| `Profit` | Прибыль по закрытой сделке |
| `ProfitByPrice` | Прибыль по переданной цене `price` |
| `Duration` | Длительность сделки с момента открытия до закрытия |
| `Extra` | Вернет дополнительные данные по ключу `key`, либо `nil`, если данные не найдены |
| `SetExtra` | Устанавливает значение `val` для ключа `key` |
| `RangeExtra` | Выполняет переданную функцию для каждого значения в списке Extra |

## Callbacks on events

Для выполнения дополнительных действий (отправка оповещений, сохранение позиции в БД и т. п.) торговый движок предоставляется методы, с помощью которых можно установить колбэки. Методы не потокобезопасны, вызывать следует до запуска стратегии в работу. 

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

* Реализовать инициализацию торгового движка открытыми сделками.



