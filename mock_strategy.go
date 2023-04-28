// Code generated by mockery v2.20.2. DO NOT EDIT.

package trengin

import (
	context "context"

	mock "github.com/stretchr/testify/mock"
)

// MockStrategy is an autogenerated mock type for the Strategy type
type MockStrategy struct {
	mock.Mock
}

// Run provides a mock function with given fields: ctx, actions
func (_m *MockStrategy) Run(ctx context.Context, actions Actions) error {
	ret := _m.Called(ctx, actions)

	var r0 error
	if rf, ok := ret.Get(0).(func(context.Context, Actions) error); ok {
		r0 = rf(ctx, actions)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

type mockConstructorTestingTNewMockStrategy interface {
	mock.TestingT
	Cleanup(func())
}

// NewMockStrategy creates a new instance of MockStrategy. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewMockStrategy(t mockConstructorTestingTNewMockStrategy) *MockStrategy {
	mock := &MockStrategy{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
