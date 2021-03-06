# trengin

[![Lint Status](https://github.com/evsamsonov/trengin/actions/workflows/lint.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=golangci-lint)
[![Test Status](https://github.com/evsamsonov/trengin/actions/workflows/test.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=test)
[![Go Report Card](https://goreportcard.com/badge/github.com/evsamsonov/trengin)](https://goreportcard.com/report/github.com/evsamsonov/trengin)
[![codecov](https://codecov.io/gh/evsamsonov/trengin/branch/master/graph/badge.svg?token=AC751PKE5Y)](https://codecov.io/gh/evsamsonov/trengin)

Golang библиотека для создания торгового робота. Связывает торговую стратегию и реализацию исполнения торговых операций. Позволяет гибко описать стратегию.

## Содержание 

- [Установка](#установка)
- [Как использовать](#как-использовать)
- [Основные сущности](#основные-сущности)
- [Как реализовать торговую стратегию](#как-реализовать-торговую-стратегию)
- [Как реализовать модуль исполнения торговых операций](#как-реализовать-модуль-исполнения-торговых-операций)
- [Описание сущности Position](#описание-сущности-position)
- [Дополнительные действия на события](#дополнительные-действия-на-события)
- [Что дальше](#что-дальше)

## Установка

```shell
go get github.com/evsamsonov/trengin
```

## Как использовать

Импортировать пакет.

```go
import "github.com/evsamsonov/trengin"
```

Создать экземпляр, передав объекты реализующие интерфейс Strategy и Broker.

```go

tradingEngine := trengin.New(strategy, broker)
tradingEngine.Run()
```

## Основные сущности

|  Название  | Описание | 
| ------------- | ------------- | 
| `Engine`  | Торговый движок  |
| `Strategy`  | Торговая стратегия  |
| `Broker`  | Модуль исполнения торговых операций  |
| `Actions`  | Канал для отправки торговых действий  |
| `Position`  | Торговая позиция  |
| `PositionClosed` | Канал, в который будет отправлена позиция при закрытии |

## Как реализовать торговую стратегию

Торговая стратегия описана следующим интерфейсом.
```go
type Strategy interface {
	Run(ctx context.Context)
	Actions() Actions
	Errors() <-chan error
}
```

В методе `Run` реализуется логика торговой стратегии. Здесь может быть анализ текущих данных, открытие позиции, анализ данных по открытой позиции для закрытия или для изменения условной заявки (перестановка стопа в безубыток, трейлинг стоп и т. п.).

Метод `Actions` должен вернуть канал через который будет происходить отправка торговых действий. Если закрыть канал, то торговый движок завершит свою работу.

Метод `Errors` должен вернуть канал для отправки критических ошибок в работе стратегии. Если закрыть канал или передать в него ошибку, торговый движок завершит свою работу. 

В канал торговых действий можно отправить экземпляры `OpenPositionAction`, `ClosePositionAction`, `ChangeConditionalOrderAction`. Создаются через конструкторы. При отправке неожидаемых типов торговый движок завершит работу с ошибкой.

### OpenPositionAction

Открытие позиции.

Конструктор: `NewOpenPositionAction`

| Параметр | Описание |
| ------------- | ------------- |
| `positionType` | Тип позиции (покупка или продажа) |
| `stopLossIndent` | Отступ стоп-лосса от цены открытия позиции |
| `takeProfitIndent` | Отступ тейк-профита от цены открытия позиции  |

### ChangeConditionalOrderAction

Изменение условной заявки.

Конструктор: `NewChangeConditionalOrderAction`

| Наименование | Описание |
| ------------- | ------------- |
| `positionID` | Идентификатор позиции |
| `stopLoss` | Новое значения для стоп-лосса (если равно 0, то не изменять) |
| `takeProfit` | Новое значения для стоп-лосса (если равно 0, то не изменять) |

### ClosePositionAction

Закрытие позиции.

Конструктор: `NewClosePositionAction`

| Наименование | Описание |
| ------------- | ------------- |
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

## Описание сущности Position

Структура `Position` описывает торговую позицию. Содержит уникальный в рамках одного запуска идентификатор `ID` , основные и дополнительные данные о позиции. Может быть в двух состояниях — открытом и закрытом. Некоторые методы возвращают корректное значение только при закрытой позиции. 

Дополнительные данные `Extra` следует использовать только в информационных целях, не завязывая на них логику работы стратегии и модуля исполнения торговых операций. Кроме случаев локального использования.  

Создается через конструктор `NewPosition` по `action` с временем открытия `openTime` и ценой открытия `openPrice`. Позиция должна создаваться и закрываться в реализации `Broker`. В реализацию `Strategy` передается копия позиции с возможностью установить дополнительные данные.

**Поля**

| Название | Описание |
| ------------- | ------------- |
| `ID` | Уникальный идентификатор в рамках запуска |
| `Type` | Тип |
| `OpenTime` | Время открытия |
| `OpenPrice` | Цена открытия |
| `CloseTime` | Время закрытия |
| `ClosePrice` | Цена закрытия |
| `StopLoss` | Текущий стоп лосс |
| `TakeProfit` | Текущий тейк профит |

**Методы**

|  Название  | Описание | 
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

## Дополнительные действия на события

Для выполнения дополнительных действий (отправка оповещений, сохранение позиции в БД и т. п.) торговый движок предоставляется методы, с помощью которых можно установить колбэки. Методы не потокобезопасны, вызывать следует до запуска стратегии в работу. 

|  Метод | Описание |
| ------------- | ------------- |
| OnPositionOpened  | Устанавливает коллбек на открытие позиции  |
| OnConditionalOrderChanged  | Устанавливает коллбек на изменение условной заявки  |
| OnPositionClosed  | Устанавливает коллбек на закрытие позиции |

## Что дальше

* Реализовать модули исполнения торговых операций для различных торговых систем.
* Добавить возможность открывать позиции по разным торговым инструментам.


