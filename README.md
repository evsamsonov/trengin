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

### Основные сущности

|  Название  | Описание | 
| ------------- | ------------- | 
| Engine  | Торговый движок  |
| Strategy  | Торговая стратегия  |
| Broker  | Клиент, реализующего исполнения торговых операций  |
| Actions  | Канал для отправки торговых действий  |


### Как реализовать торговую стратегию

Торговая стратегия описана следующим интерфейсом
```go
type Strategy interface {
	Run(ctx context.Context)
	Actions() Actions
	Errors() <-chan error
}
```

В методе Run требуется реализовать логику торговой стратегию. Здесь может быть анализ текущих данных и открытие позиции, анализ данных по открытой позиции для её закрытия или для изменения условной заявки (перестановка стопа в безубыток, трейлинг стоп и т.п.).

Метод Actions должен вернуть канал, который можно использовать для отправки торговых действий. Если закрыть канал, то торговый движок завершит свою работу. Далее канал торговых действий???

Метод Errors должен вернуть канал для отправки критических ошибок в работе стратегии. Если закрыть канал или передать в него ошибку, торговый движок завершит свою работу. 

В канал торговых действий можно отправить экземпляры OpenPositionAction, ClosePositionAction, ChangeConditionalOrderAction. Создать их можно через конструктор. При отправке неожиданных типов торговый движок завершит работу с ошибкой.

**OpenPositionAction**

Открытие позиции

Конструктор: NewOpenPositionAction

Параметры

| Наименование | Описание |
| ------------- | ------------- |
| positionType | Тип позиции (Long или Short) |
| stopLossIndent | Отступ стоп-лосса от цены открытия позиции |
| takeProfitIndent | Отступ тейк-профита от цены открытия позиции  |

**ChangeConditionalOrderAction**

Изменение условной заявки

Конструктор: NewChangeConditionalOrderAction

Параметры

| Наименование | Описание |
| ------------- | ------------- |
| positionID | Идентификатор позиции |
| stopLoss | Новое значения для стоп-лосса (если равны 0, то не изменять) |
| takeProfit | Новое значения для стоп-лосса (если равны 0, то не изменять) |

**ClosePositionAction**

Закрытие позиции

Конструктор: NewClosePositionAction

Параметры

| Наименование | Описание |
| ------------- | ------------- |
| positionID | Идентификатор позиции |

Пример отправки действия и получения результата

```go
action := trengin.NewOpenPositionAction(positionType, stopLossIndent, takeProfitIndent)
s.actions <- action
openPositionResult := <-act.Result()
```

Стратегия может завершить работу торгового движка по своей инициативе, закрыв канал Actions, отправив ошибку в канал с ошибками или закрыв канал с ошибками. 

### Как реализовать клиента для отправки торговых операций

Клиент, реализующий взаимодействие исполнения торговых операций, должен реализовывать следующий интерфейс

```go
type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}
```

Broker может запускать свои горутины, например, для отслеживания открытой позиции. Необходимо завершить все горутины при закрытии канала ctx.Done() во избежании утечки горутин.  

### Дополнительные действия на события

|  Методы  |   |
| ------------- | ------------- |
| OnPositionOpened  | Устанавливает коллбек на открытие позиции  |
| OnConditionalOrderChanged  | Устанавливает коллбек на изменение условной заявки  |
| OnPositionClosed  | Устанавливает коллбек на закрытие позиции |


