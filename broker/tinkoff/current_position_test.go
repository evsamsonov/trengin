package tinkoff

import (
	"testing"
	"time"

	"bou.ke/monkey"
	"github.com/stretchr/testify/assert"

	"github.com/evsamsonov/trengin"
)

func Test_currentPosition_Close(t *testing.T) {
	pos, err := trengin.NewPosition(
		trengin.OpenPositionAction{
			Type:     trengin.Long,
			Quantity: 2,
		},
		time.Now(),
		1,
	)
	assert.NoError(t, err)

	now := time.Now()
	monkey.Patch(time.Now, func() time.Time {
		return now
	})

	closed := make(chan trengin.Position, 1)
	currentPosition := currentPosition{
		position:     pos,
		stopLossID:   "2",
		takeProfitID: "3",
		closed:       closed,
	}
	err = currentPosition.Close(123.45)
	assert.NoError(t, err)
	assert.Equal(t, "", currentPosition.StopLossID())
	assert.Equal(t, "", currentPosition.TakeProfitID())
	assert.Nil(t, currentPosition.Position())

	select {
	case pos := <-closed:
		assert.Equal(t, 123.45, pos.ClosePrice)
		assert.Equal(t, now, pos.CloseTime)
	default:
		t.Fatal("Position not send")
	}
}
