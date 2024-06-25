// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/grafana/flagger-k6-webhook/pkg/k6 (interfaces: Client,TestRun)

// Package mocks is a generated GoMock package.
package mocks

import (
	context "context"
	io "io"
	reflect "reflect"
	time "time"

	gomock "github.com/golang/mock/gomock"
	k6 "github.com/grafana/flagger-k6-webhook/pkg/k6"
)

// MockK6Client is a mock of Client interface.
type MockK6Client struct {
	ctrl     *gomock.Controller
	recorder *MockK6ClientMockRecorder
}

// MockK6ClientMockRecorder is the mock recorder for MockK6Client.
type MockK6ClientMockRecorder struct {
	mock *MockK6Client
}

// NewMockK6Client creates a new mock instance.
func NewMockK6Client(ctrl *gomock.Controller) *MockK6Client {
	mock := &MockK6Client{ctrl: ctrl}
	mock.recorder = &MockK6ClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockK6Client) EXPECT() *MockK6ClientMockRecorder {
	return m.recorder
}

// Start mocks base method.
func (m *MockK6Client) Start(arg0 context.Context, arg1 string, arg2 bool, arg3 map[string]string, arg4 io.Writer) (k6.TestRun, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Start", arg0, arg1, arg2, arg3, arg4)
	ret0, _ := ret[0].(k6.TestRun)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Start indicates an expected call of Start.
func (mr *MockK6ClientMockRecorder) Start(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Start", reflect.TypeOf((*MockK6Client)(nil).Start), arg0, arg1, arg2, arg3, arg4)
}

// MockK6TestRun is a mock of TestRun interface.
type MockK6TestRun struct {
	ctrl     *gomock.Controller
	recorder *MockK6TestRunMockRecorder
}

// MockK6TestRunMockRecorder is the mock recorder for MockK6TestRun.
type MockK6TestRunMockRecorder struct {
	mock *MockK6TestRun
}

// NewMockK6TestRun creates a new mock instance.
func NewMockK6TestRun(ctrl *gomock.Controller) *MockK6TestRun {
	mock := &MockK6TestRun{ctrl: ctrl}
	mock.recorder = &MockK6TestRunMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockK6TestRun) EXPECT() *MockK6TestRunMockRecorder {
	return m.recorder
}

// ExecutionDuration mocks base method.
func (m *MockK6TestRun) ExecutionDuration() time.Duration {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ExecutionDuration")
	ret0, _ := ret[0].(time.Duration)
	return ret0
}

// ExecutionDuration indicates an expected call of ExecutionDuration.
func (mr *MockK6TestRunMockRecorder) ExecutionDuration() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ExecutionDuration", reflect.TypeOf((*MockK6TestRun)(nil).ExecutionDuration))
}

// ExitCode mocks base method.
func (m *MockK6TestRun) ExitCode() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ExitCode")
	ret0, _ := ret[0].(int)
	return ret0
}

// ExitCode indicates an expected call of ExitCode.
func (mr *MockK6TestRunMockRecorder) ExitCode() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ExitCode", reflect.TypeOf((*MockK6TestRun)(nil).ExitCode))
}

// Exited mocks base method.
func (m *MockK6TestRun) Exited() bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Exited")
	ret0, _ := ret[0].(bool)
	return ret0
}

// Exited indicates an expected call of Exited.
func (mr *MockK6TestRunMockRecorder) Exited() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Exited", reflect.TypeOf((*MockK6TestRun)(nil).Exited))
}

// Kill mocks base method.
func (m *MockK6TestRun) Kill() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Kill")
	ret0, _ := ret[0].(error)
	return ret0
}

// Kill indicates an expected call of Kill.
func (mr *MockK6TestRunMockRecorder) Kill() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Kill", reflect.TypeOf((*MockK6TestRun)(nil).Kill))
}

// PID mocks base method.
func (m *MockK6TestRun) PID() int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PID")
	ret0, _ := ret[0].(int)
	return ret0
}

// PID indicates an expected call of PID.
func (mr *MockK6TestRunMockRecorder) PID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PID", reflect.TypeOf((*MockK6TestRun)(nil).PID))
}

// Wait mocks base method.
func (m *MockK6TestRun) Wait() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Wait")
	ret0, _ := ret[0].(error)
	return ret0
}

// Wait indicates an expected call of Wait.
func (mr *MockK6TestRunMockRecorder) Wait() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Wait", reflect.TypeOf((*MockK6TestRun)(nil).Wait))
}
