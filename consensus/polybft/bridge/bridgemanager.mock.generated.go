// Code generated by mockery v2.46.3. DO NOT EDIT.

package bridge

import (
	big "math/big"

	bbolt "go.etcd.io/bbolt"

	config "github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	ethgo "github.com/Ethernal-Tech/ethgo"

	mock "github.com/stretchr/testify/mock"

	oracle "github.com/0xPolygon/polygon-edge/consensus/polybft/oracle"

	types "github.com/0xPolygon/polygon-edge/types"
)

// BridgeManagerMock is an autogenerated mock type for the BridgeManager type
type BridgeManagerMock struct {
	mock.Mock
}

type BridgeManagerMock_Expecter struct {
	mock *mock.Mock
}

func (_m *BridgeManagerMock) EXPECT() *BridgeManagerMock_Expecter {
	return &BridgeManagerMock_Expecter{mock: &_m.Mock}
}

// AddLog provides a mock function with given fields: chainID, eventLog
func (_m *BridgeManagerMock) AddLog(chainID *big.Int, eventLog *ethgo.Log) error {
	ret := _m.Called(chainID, eventLog)

	if len(ret) == 0 {
		panic("no return value specified for AddLog")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(*big.Int, *ethgo.Log) error); ok {
		r0 = rf(chainID, eventLog)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// BridgeManagerMock_AddLog_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'AddLog'
type BridgeManagerMock_AddLog_Call struct {
	*mock.Call
}

// AddLog is a helper method to define mock.On call
//   - chainID *big.Int
//   - eventLog *ethgo.Log
func (_e *BridgeManagerMock_Expecter) AddLog(chainID interface{}, eventLog interface{}) *BridgeManagerMock_AddLog_Call {
	return &BridgeManagerMock_AddLog_Call{Call: _e.mock.On("AddLog", chainID, eventLog)}
}

func (_c *BridgeManagerMock_AddLog_Call) Run(run func(chainID *big.Int, eventLog *ethgo.Log)) *BridgeManagerMock_AddLog_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*big.Int), args[1].(*ethgo.Log))
	})
	return _c
}

func (_c *BridgeManagerMock_AddLog_Call) Return(_a0 error) *BridgeManagerMock_AddLog_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *BridgeManagerMock_AddLog_Call) RunAndReturn(run func(*big.Int, *ethgo.Log) error) *BridgeManagerMock_AddLog_Call {
	_c.Call.Return(run)
	return _c
}

// BridgeBatch provides a mock function with given fields: blockNumber
func (_m *BridgeManagerMock) BridgeBatch(blockNumber uint64) (*BridgeBatchSigned, error) {
	ret := _m.Called(blockNumber)

	if len(ret) == 0 {
		panic("no return value specified for BridgeBatch")
	}

	var r0 *BridgeBatchSigned
	var r1 error
	if rf, ok := ret.Get(0).(func(uint64) (*BridgeBatchSigned, error)); ok {
		return rf(blockNumber)
	}
	if rf, ok := ret.Get(0).(func(uint64) *BridgeBatchSigned); ok {
		r0 = rf(blockNumber)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*BridgeBatchSigned)
		}
	}

	if rf, ok := ret.Get(1).(func(uint64) error); ok {
		r1 = rf(blockNumber)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// BridgeManagerMock_BridgeBatch_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'BridgeBatch'
type BridgeManagerMock_BridgeBatch_Call struct {
	*mock.Call
}

// BridgeBatch is a helper method to define mock.On call
//   - blockNumber uint64
func (_e *BridgeManagerMock_Expecter) BridgeBatch(blockNumber interface{}) *BridgeManagerMock_BridgeBatch_Call {
	return &BridgeManagerMock_BridgeBatch_Call{Call: _e.mock.On("BridgeBatch", blockNumber)}
}

func (_c *BridgeManagerMock_BridgeBatch_Call) Run(run func(blockNumber uint64)) *BridgeManagerMock_BridgeBatch_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(uint64))
	})
	return _c
}

func (_c *BridgeManagerMock_BridgeBatch_Call) Return(_a0 *BridgeBatchSigned, _a1 error) *BridgeManagerMock_BridgeBatch_Call {
	_c.Call.Return(_a0, _a1)
	return _c
}

func (_c *BridgeManagerMock_BridgeBatch_Call) RunAndReturn(run func(uint64) (*BridgeBatchSigned, error)) *BridgeManagerMock_BridgeBatch_Call {
	_c.Call.Return(run)
	return _c
}

// Close provides a mock function with given fields:
func (_m *BridgeManagerMock) Close() {
	_m.Called()
}

// BridgeManagerMock_Close_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Close'
type BridgeManagerMock_Close_Call struct {
	*mock.Call
}

// Close is a helper method to define mock.On call
func (_e *BridgeManagerMock_Expecter) Close() *BridgeManagerMock_Close_Call {
	return &BridgeManagerMock_Close_Call{Call: _e.mock.On("Close")}
}

func (_c *BridgeManagerMock_Close_Call) Run(run func()) *BridgeManagerMock_Close_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *BridgeManagerMock_Close_Call) Return() *BridgeManagerMock_Close_Call {
	_c.Call.Return()
	return _c
}

func (_c *BridgeManagerMock_Close_Call) RunAndReturn(run func()) *BridgeManagerMock_Close_Call {
	_c.Call.Return(run)
	return _c
}

// GetLogFilters provides a mock function with given fields:
func (_m *BridgeManagerMock) GetLogFilters() map[types.Address][]types.Hash {
	ret := _m.Called()

	if len(ret) == 0 {
		panic("no return value specified for GetLogFilters")
	}

	var r0 map[types.Address][]types.Hash
	if rf, ok := ret.Get(0).(func() map[types.Address][]types.Hash); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(map[types.Address][]types.Hash)
		}
	}

	return r0
}

// BridgeManagerMock_GetLogFilters_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'GetLogFilters'
type BridgeManagerMock_GetLogFilters_Call struct {
	*mock.Call
}

// GetLogFilters is a helper method to define mock.On call
func (_e *BridgeManagerMock_Expecter) GetLogFilters() *BridgeManagerMock_GetLogFilters_Call {
	return &BridgeManagerMock_GetLogFilters_Call{Call: _e.mock.On("GetLogFilters")}
}

func (_c *BridgeManagerMock_GetLogFilters_Call) Run(run func()) *BridgeManagerMock_GetLogFilters_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *BridgeManagerMock_GetLogFilters_Call) Return(_a0 map[types.Address][]types.Hash) *BridgeManagerMock_GetLogFilters_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *BridgeManagerMock_GetLogFilters_Call) RunAndReturn(run func() map[types.Address][]types.Hash) *BridgeManagerMock_GetLogFilters_Call {
	_c.Call.Return(run)
	return _c
}

// PostBlock provides a mock function with given fields: req
func (_m *BridgeManagerMock) PostBlock(req *oracle.PostBlockRequest) error {
	ret := _m.Called(req)

	if len(ret) == 0 {
		panic("no return value specified for PostBlock")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(*oracle.PostBlockRequest) error); ok {
		r0 = rf(req)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// BridgeManagerMock_PostBlock_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'PostBlock'
type BridgeManagerMock_PostBlock_Call struct {
	*mock.Call
}

// PostBlock is a helper method to define mock.On call
//   - req *oracle.PostBlockRequest
func (_e *BridgeManagerMock_Expecter) PostBlock(req interface{}) *BridgeManagerMock_PostBlock_Call {
	return &BridgeManagerMock_PostBlock_Call{Call: _e.mock.On("PostBlock", req)}
}

func (_c *BridgeManagerMock_PostBlock_Call) Run(run func(req *oracle.PostBlockRequest)) *BridgeManagerMock_PostBlock_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*oracle.PostBlockRequest))
	})
	return _c
}

func (_c *BridgeManagerMock_PostBlock_Call) Return(_a0 error) *BridgeManagerMock_PostBlock_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *BridgeManagerMock_PostBlock_Call) RunAndReturn(run func(*oracle.PostBlockRequest) error) *BridgeManagerMock_PostBlock_Call {
	_c.Call.Return(run)
	return _c
}

// PostEpoch provides a mock function with given fields: req
func (_m *BridgeManagerMock) PostEpoch(req *oracle.PostEpochRequest) error {
	ret := _m.Called(req)

	if len(ret) == 0 {
		panic("no return value specified for PostEpoch")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(*oracle.PostEpochRequest) error); ok {
		r0 = rf(req)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// BridgeManagerMock_PostEpoch_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'PostEpoch'
type BridgeManagerMock_PostEpoch_Call struct {
	*mock.Call
}

// PostEpoch is a helper method to define mock.On call
//   - req *oracle.PostEpochRequest
func (_e *BridgeManagerMock_Expecter) PostEpoch(req interface{}) *BridgeManagerMock_PostEpoch_Call {
	return &BridgeManagerMock_PostEpoch_Call{Call: _e.mock.On("PostEpoch", req)}
}

func (_c *BridgeManagerMock_PostEpoch_Call) Run(run func(req *oracle.PostEpochRequest)) *BridgeManagerMock_PostEpoch_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*oracle.PostEpochRequest))
	})
	return _c
}

func (_c *BridgeManagerMock_PostEpoch_Call) Return(_a0 error) *BridgeManagerMock_PostEpoch_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *BridgeManagerMock_PostEpoch_Call) RunAndReturn(run func(*oracle.PostEpochRequest) error) *BridgeManagerMock_PostEpoch_Call {
	_c.Call.Return(run)
	return _c
}

// ProcessLog provides a mock function with given fields: header, log, dbTx
func (_m *BridgeManagerMock) ProcessLog(header *types.Header, log *ethgo.Log, dbTx *bbolt.Tx) error {
	ret := _m.Called(header, log, dbTx)

	if len(ret) == 0 {
		panic("no return value specified for ProcessLog")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(*types.Header, *ethgo.Log, *bbolt.Tx) error); ok {
		r0 = rf(header, log, dbTx)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// BridgeManagerMock_ProcessLog_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'ProcessLog'
type BridgeManagerMock_ProcessLog_Call struct {
	*mock.Call
}

// ProcessLog is a helper method to define mock.On call
//   - header *types.Header
//   - log *ethgo.Log
//   - dbTx *bbolt.Tx
func (_e *BridgeManagerMock_Expecter) ProcessLog(header interface{}, log interface{}, dbTx interface{}) *BridgeManagerMock_ProcessLog_Call {
	return &BridgeManagerMock_ProcessLog_Call{Call: _e.mock.On("ProcessLog", header, log, dbTx)}
}

func (_c *BridgeManagerMock_ProcessLog_Call) Run(run func(header *types.Header, log *ethgo.Log, dbTx *bbolt.Tx)) *BridgeManagerMock_ProcessLog_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*types.Header), args[1].(*ethgo.Log), args[2].(*bbolt.Tx))
	})
	return _c
}

func (_c *BridgeManagerMock_ProcessLog_Call) Return(_a0 error) *BridgeManagerMock_ProcessLog_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *BridgeManagerMock_ProcessLog_Call) RunAndReturn(run func(*types.Header, *ethgo.Log, *bbolt.Tx) error) *BridgeManagerMock_ProcessLog_Call {
	_c.Call.Return(run)
	return _c
}

// Start provides a mock function with given fields: runtimeCfg
func (_m *BridgeManagerMock) Start(runtimeCfg *config.Runtime) error {
	ret := _m.Called(runtimeCfg)

	if len(ret) == 0 {
		panic("no return value specified for Start")
	}

	var r0 error
	if rf, ok := ret.Get(0).(func(*config.Runtime) error); ok {
		r0 = rf(runtimeCfg)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// BridgeManagerMock_Start_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Start'
type BridgeManagerMock_Start_Call struct {
	*mock.Call
}

// Start is a helper method to define mock.On call
//   - runtimeCfg *config.Runtime
func (_e *BridgeManagerMock_Expecter) Start(runtimeCfg interface{}) *BridgeManagerMock_Start_Call {
	return &BridgeManagerMock_Start_Call{Call: _e.mock.On("Start", runtimeCfg)}
}

func (_c *BridgeManagerMock_Start_Call) Run(run func(runtimeCfg *config.Runtime)) *BridgeManagerMock_Start_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(*config.Runtime))
	})
	return _c
}

func (_c *BridgeManagerMock_Start_Call) Return(_a0 error) *BridgeManagerMock_Start_Call {
	_c.Call.Return(_a0)
	return _c
}

func (_c *BridgeManagerMock_Start_Call) RunAndReturn(run func(*config.Runtime) error) *BridgeManagerMock_Start_Call {
	_c.Call.Return(run)
	return _c
}

// NewBridgeManagerMock creates a new instance of BridgeManagerMock. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewBridgeManagerMock(t interface {
	mock.TestingT
	Cleanup(func())
}) *BridgeManagerMock {
	mock := &BridgeManagerMock{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
