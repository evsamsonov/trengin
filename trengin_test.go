package trengin

import (
	"fmt"
	"testing"
	"time"
)

func TestPosition_Extra(t *testing.T) {
	pos := NewPosition(OpenPositionAction{}, time.Unix(1, 0), 12345)
	pos.AddExtra("test", 111)
	fmt.Println(pos.Extra("test"))
}
