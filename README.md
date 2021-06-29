# trengin

[![Lint Status](https://github.com/evsamsonov/trengin/actions/workflows/lint.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=golangci-lint)
[![Test Status](https://github.com/evsamsonov/trengin/actions/workflows/test.yml/badge.svg)](https://github.com/evsamsonov/trengin/actions?workflow=test)

Основа для создания автоматизированного торгового робота. Связывает торговую стратегию и клиента, реализующего исполнение торговых операций. Позволяет гибко реализовать торговую стратегию.

## Установка

```shell
go get github.com/evsamsonov/trengin
```

## Как использовать

Импортировать пакет 

```go
import "github.com/evsamsonov/trengin"
```

Создать экземпляр, передав объекты реализующие интерфейс стратегии и брокера

```go

tradingEngine := trengin.New(strategy, broker)
tradingEngine.Run()
```

### Стратегия

Стратегия должна реализовывать следующий интерфейс
```go
type Strategy interface {
	Run(ctx context.Context)
	Actions() Actions
	Errors() <-chan error
}
```

Метод Run выполняется при старте. Стратегия может отправлять сообщения в канал Actions. Возможны следующие типы сообщений. Стратегия должна завершать свою работу при закрытии канала контекста ctx.Done()
- открыть сделку
- закрыть сделку
- изменить стоп-лосс

Стратегия может завершить работу по своей инициативе, закрыв канал Actions, отправив ошибку в канал с ошибками или закрыв канал с ошибками 


### Брокер

Клиент, реализующий взаимодействие исполнения торговых операций, должен реализовывать следующий интерфейс

```go
type Broker interface {
	OpenPosition(ctx context.Context, action OpenPositionAction) (Position, PositionClosed, error)
	ClosePosition(ctx context.Context, action ClosePositionAction) (Position, error)
	ChangeConditionalOrder(ctx context.Context, action ChangeConditionalOrderAction) (Position, error)
}
```

Broker может запускать свои горутины, например, для отслеживания открытой позиции. Необходимо завершить все горутины при закрытии канала ctx.Done() во избежании утечки горутин.  




