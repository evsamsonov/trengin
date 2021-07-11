# trengin

[![Lint Status](https://github.com/evsamsonov/trengin/actions/workflows/lint.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=golangci-lint)
[![Test Status](https://github.com/evsamsonov/trengin/actions/workflows/test.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=test)

Golang библиотека для создания торгового робота. Связывает торговую стратегию и реализацию исполнения торговых операций. Позволяет гибко описать стратегию.

## Установка

```shell
go get github.com/evsamsonov/trengin
```

## Как использовать

Импортировать пакет 

```go
import "github.com/evsamsonov/trengin"
```

Создать экземпляр, передав объекты реализующие интерфейс Strategy и Broker

```go

tradingEngine := trengin.New(strategy, broker)
tradingEngine.Run()
```

## Основные сущности

|  Название  | Описание | 
| ------------- | ------------- | 
| Engine  | Торговый движок  |
| Strategy  | Торговая стратегия  |
| Broker  | Модуль исполнения торговых операций  |
| Actions  | Канал для отправки торговых действий  |


## Как реализовать торговую стратегию

Торговая стратегия описана следующим интерфейсом
```go
type Strategy interface {
	Run(ctx context.Context)
	Actions() Actions
	Errors() <-chan error
}
```

В методе `Run` требуется реализовать логику торговой стратегию. Здесь может быть анализ текущих данных и открытие позиции, анализ данных по открытой позиции для её закрытия или для изменения условной заявки (перестановка стопа в безубыток, трейлинг стоп и т. п.).

Метод `Actions` должен вернуть канал через который будет происходить отправка торговых действий. Если закрыть канал, то торговый движок завершит свою работу.

Метод `Errors` должен вернуть канал для отправки критических ошибок в работе стратегии. Если закрыть канал или передать в него ошибку, торговый движок завершит свою работу. 

В канал торговых действий можно отправить экземпляры `OpenPositionAction`, `ClosePositionAction`, `ChangeConditionalOrderAction`. Создаются через конструкторы. При отправке неожиданных типов торговый движок завершит работу с ошибкой.

**OpenPositionAction**

Открытие позиции

Конструктор: `NewOpenPositionAction`

| Параметр | Описание |
| ------------- | ------------- |
| `positionType` | Тип позиции (Long или Short) |
| `stopLossIndent` | Отступ стоп-лосса от цены открытия позиции |
| `takeProfitIndent` | Отступ тейк-профита от цены открытия позиции  |

**ChangeConditionalOrderAction**

Изменение условной заявки

Конструктор: `NewChangeConditionalOrderAction`

| Наименование | Описание |
| ------------- | ------------- |
| `positionID` | Идентификатор позиции |
| `stopLoss` | Новое значения для стоп-лосса (если равны 0, то не изменять) |
| `takeProfit` | Новое значения для стоп-лосса (если равны 0, то не изменять) |

**ClosePositionAction**

Закрытие позиции

Конструктор: `NewClosePositionAction`

| Наименование | Описание |
| ------------- | ------------- |
| `positionID` | Идентификатор позиции |

Пример отправки действия и получения результата

```go
action := trengin.NewOpenPositionAction(positionType, stopLossIndent, takeProfitIndent)
s.actions <- action
openPositionResult := <-act.Result()
```

## Как реализовать модуль для отправки торговых операций

Описан следующим интерфейсом

```go
type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}
```

Метод `OpenPosition` должен открывать новую позицию по данным `action`, возвращать экземпляр открытой позиции и канал, в который будет записана позиция при её закрытии. Должен реализовывать отслеживания закрытия позиции по условной заявке. 

Метод `ClosePosition` должен закрывать позицию по данным `action`. Возвращать экземпляр закрытой позиции. 

Метод `ChangeConditionalOrder` должен изменить условную заявку по данным `action`. Возвращать актуальный экземпляр позиции.


## Дополнительные действия на события

Для выполнения дополнительных действий (отправка оповещений, сохранение позици в бд и т. п.) торговый движок предоставляется методы, с помощью которых можно установить колбэки. Методы не потокобезопасны, вызывать следует до запуcка стратегии в работу. 

|  Метод | Описание |
| ------------- | ------------- |
| OnPositionOpened  | Устанавливает коллбек на открытие позиции  |
| OnConditionalOrderChanged  | Устанавливает коллбек на изменение условной заявки  |
| OnPositionClosed  | Устанавливает коллбек на закрытие позиции |


