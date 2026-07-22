package main

import (
	"context"
	"fmt"

	"github.com/evsamsonov/trengin/v2"
)

const demoFIGI = "BBG004730N88"

type demoStrategy struct{}

func (s demoStrategy) Run(ctx context.Context, actions trengin.Actions) error {
	openAction := trengin.NewOpenPositionAction(demoFIGI, trengin.Long, 1, 5, 10)
	if err := sendAction(ctx, actions, openAction); err != nil {
		return fmt.Errorf("send open action: %w", err)
	}

	openResult, err := openAction.Result(ctx)
	if err != nil {
		return fmt.Errorf("open position result: %w", err)
	}

	changeAction := trengin.NewChangeConditionalOrderAction(
		openResult.Position.ID,
		openResult.Position.OpenPrice-3,
		openResult.Position.OpenPrice+12,
	)
	if err = sendAction(ctx, actions, changeAction); err != nil {
		return fmt.Errorf("send change action: %w", err)
	}

	if _, err = changeAction.Result(ctx); err != nil {
		return fmt.Errorf("change conditional order result: %w", err)
	}

	closeAction := trengin.NewClosePositionAction(openResult.Position.ID)
	if err = sendAction(ctx, actions, closeAction); err != nil {
		return fmt.Errorf("send close action: %w", err)
	}

	if _, err = closeAction.Result(ctx); err != nil {
		return fmt.Errorf("close position result: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait position closed: %w", ctx.Err())
	case <-openResult.Closed:
	}

	close(actions)
	<-ctx.Done()
	return nil
}

func sendAction(ctx context.Context, actions trengin.Actions, action interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case actions <- action:
		return nil
	}
}
