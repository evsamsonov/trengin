package tinkoff

import (
	"sync"
	"time"

	"github.com/evsamsonov/trengin"
)

type currentPosition struct {
	position     *trengin.Position
	stopLossID   string
	takeProfitID string
	closed       chan trengin.Position
	mtx          sync.RWMutex
}

func (p *currentPosition) Set(
	position *trengin.Position,
	stopLossID,
	takeProfitID string,
	closed chan trengin.Position,
) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	p.position = position
	p.stopLossID = stopLossID
	p.takeProfitID = takeProfitID
	p.closed = closed
}

func (p *currentPosition) Exist() bool {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return p.position != nil
}

func (p *currentPosition) Position() *trengin.Position {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return p.position
}

func (p *currentPosition) StopLossID() string {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return p.stopLossID
}

func (p *currentPosition) TakeProfitID() string {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	return p.takeProfitID
}

func (p *currentPosition) SetStopLossID(stopLossID string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	p.stopLossID = stopLossID
}

func (p *currentPosition) SetTakeProfitID(takeProfitID string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	p.takeProfitID = takeProfitID
}

func (p *currentPosition) SetQuantity(quantity int64) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	p.position.Quantity = quantity
}

func (p *currentPosition) Close(closePrice float64) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if err := p.position.Close(time.Now(), closePrice); err != nil {
		return err
	}

	p.closed <- *p.position
	p.position, p.stopLossID, p.takeProfitID = nil, "", ""
	return nil
}
