// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package basket

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
	_ = time.Tick
	_ = context.Background
)

// Component is an auto generated low-level Go binding around an user-defined struct.
type Component struct {
	Market    common.Address
	WeightBps uint16
}

// SweemBasketMetaData contains all meta data concerning the SweemBasket contract.
var SweemBasketMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"name_\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"symbol_\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"comps\",\"type\":\"tuple[]\",\"internalType\":\"structComponent[]\",\"components\":[{\"name\":\"market\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"weightBps\",\"type\":\"uint16\",\"internalType\":\"uint16\"}]}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"accountCore\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractIAccountCore\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"accountId\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint40\",\"internalType\":\"uint40\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"allowance\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"spender\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"approve\",\"inputs\":[{\"name\":\"spender\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"balanceOf\",\"inputs\":[{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"components\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"tuple[]\",\"internalType\":\"structComponent[]\",\"components\":[{\"name\":\"market\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"weightBps\",\"type\":\"uint16\",\"internalType\":\"uint16\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"decimals\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint8\",\"internalType\":\"uint8\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"factory\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractSweemBasketFactory\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"holdings\",\"inputs\":[],\"outputs\":[{\"name\":\"h\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"mint\",\"inputs\":[{\"name\":\"usdcIn\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minShares\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"shares\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"name\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"redeem\",\"inputs\":[{\"name\":\"shares\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minUsdcOut\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"usdcOut\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"symbol\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"tokens\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"totalSupply\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"transfer\",\"inputs\":[{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"transferFrom\",\"inputs\":[{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"usdc\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractIERC20\"}],\"stateMutability\":\"view\"},{\"type\":\"event\",\"name\":\"Approval\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"spender\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Minted\",\"inputs\":[{\"name\":\"user\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"usdcIn\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"shares\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Redeemed\",\"inputs\":[{\"name\":\"user\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"shares\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"usdcOut\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Transfer\",\"inputs\":[{\"name\":\"from\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"to\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"BadWeights\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"DuplicateMarket\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ERC20InsufficientAllowance\",\"inputs\":[{\"name\":\"spender\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"allowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"needed\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ERC20InsufficientBalance\",\"inputs\":[{\"name\":\"sender\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"balance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"needed\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ERC20InvalidApprover\",\"inputs\":[{\"name\":\"approver\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC20InvalidReceiver\",\"inputs\":[{\"name\":\"receiver\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC20InvalidSender\",\"inputs\":[{\"name\":\"sender\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC20InvalidSpender\",\"inputs\":[{\"name\":\"spender\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"Expired\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"MarketNotAllowed\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NoPrice\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"Paused\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ReentrancyGuardReentrantCall\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"SafeCastOverflowedUintDowncast\",\"inputs\":[{\"name\":\"bits\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"value\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"SafeERC20FailedOperation\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"Slippage\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ZeroAmount\",\"inputs\":[]}]",
}

// SweemBasketABI is the input ABI used to generate the binding from.
// Deprecated: Use SweemBasketMetaData.ABI instead.
var SweemBasketABI = SweemBasketMetaData.ABI

// SweemBasket is an auto generated Go binding around an Ethereum contract.
type SweemBasket struct {
	SweemBasketCaller     // Read-only binding to the contract
	SweemBasketTransactor // Write-only binding to the contract
	SweemBasketFilterer   // Log filterer for contract events
}

// SweemBasketCaller is an auto generated read-only Go binding around an Ethereum contract.
type SweemBasketCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SweemBasketTransactor is an auto generated write-only Go binding around an Ethereum contract.
type SweemBasketTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SweemBasketFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type SweemBasketFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SweemBasketSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type SweemBasketSession struct {
	Contract     *SweemBasket      // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// SweemBasketCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type SweemBasketCallerSession struct {
	Contract *SweemBasketCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts      // Call options to use throughout this session
}

// SweemBasketTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type SweemBasketTransactorSession struct {
	Contract     *SweemBasketTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts      // Transaction auth options to use throughout this session
}

// SweemBasketRaw is an auto generated low-level Go binding around an Ethereum contract.
type SweemBasketRaw struct {
	Contract *SweemBasket // Generic contract binding to access the raw methods on
}

// SweemBasketCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type SweemBasketCallerRaw struct {
	Contract *SweemBasketCaller // Generic read-only contract binding to access the raw methods on
}

// SweemBasketTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type SweemBasketTransactorRaw struct {
	Contract *SweemBasketTransactor // Generic write-only contract binding to access the raw methods on
}

// NewSweemBasket creates a new instance of SweemBasket, bound to a specific deployed contract.
func NewSweemBasket(address common.Address, backend bind.ContractBackend) (*SweemBasket, error) {
	contract, err := bindSweemBasket(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &SweemBasket{SweemBasketCaller: SweemBasketCaller{contract: contract}, SweemBasketTransactor: SweemBasketTransactor{contract: contract}, SweemBasketFilterer: SweemBasketFilterer{contract: contract}}, nil
}

// NewSweemBasketCaller creates a new read-only instance of SweemBasket, bound to a specific deployed contract.
func NewSweemBasketCaller(address common.Address, caller bind.ContractCaller) (*SweemBasketCaller, error) {
	contract, err := bindSweemBasket(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &SweemBasketCaller{contract: contract}, nil
}

// NewSweemBasketTransactor creates a new write-only instance of SweemBasket, bound to a specific deployed contract.
func NewSweemBasketTransactor(address common.Address, transactor bind.ContractTransactor) (*SweemBasketTransactor, error) {
	contract, err := bindSweemBasket(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &SweemBasketTransactor{contract: contract}, nil
}

// NewSweemBasketFilterer creates a new log filterer instance of SweemBasket, bound to a specific deployed contract.
func NewSweemBasketFilterer(address common.Address, filterer bind.ContractFilterer) (*SweemBasketFilterer, error) {
	contract, err := bindSweemBasket(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &SweemBasketFilterer{contract: contract}, nil
}

// bindSweemBasket binds a generic wrapper to an already deployed contract.
func bindSweemBasket(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := SweemBasketMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SweemBasket *SweemBasketRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SweemBasket.Contract.SweemBasketCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SweemBasket *SweemBasketRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SweemBasket.Contract.SweemBasketTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SweemBasket *SweemBasketRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SweemBasket.Contract.SweemBasketTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SweemBasket *SweemBasketCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SweemBasket.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SweemBasket *SweemBasketTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SweemBasket.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SweemBasket *SweemBasketTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SweemBasket.Contract.contract.Transact(opts, method, params...)
}

// AccountCore is a free data retrieval call binding the contract method 0x15b5b207.
//
// Solidity: function accountCore() view returns(address)
func (_SweemBasket *SweemBasketCaller) AccountCore(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "accountCore")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// AccountCore is a free data retrieval call binding the contract method 0x15b5b207.
//
// Solidity: function accountCore() view returns(address)
func (_SweemBasket *SweemBasketSession) AccountCore() (common.Address, error) {
	return _SweemBasket.Contract.AccountCore(&_SweemBasket.CallOpts)
}

// AccountCore is a free data retrieval call binding the contract method 0x15b5b207.
//
// Solidity: function accountCore() view returns(address)
func (_SweemBasket *SweemBasketCallerSession) AccountCore() (common.Address, error) {
	return _SweemBasket.Contract.AccountCore(&_SweemBasket.CallOpts)
}

// AccountId is a free data retrieval call binding the contract method 0x9cfd7cff.
//
// Solidity: function accountId() view returns(uint40)
func (_SweemBasket *SweemBasketCaller) AccountId(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "accountId")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// AccountId is a free data retrieval call binding the contract method 0x9cfd7cff.
//
// Solidity: function accountId() view returns(uint40)
func (_SweemBasket *SweemBasketSession) AccountId() (*big.Int, error) {
	return _SweemBasket.Contract.AccountId(&_SweemBasket.CallOpts)
}

// AccountId is a free data retrieval call binding the contract method 0x9cfd7cff.
//
// Solidity: function accountId() view returns(uint40)
func (_SweemBasket *SweemBasketCallerSession) AccountId() (*big.Int, error) {
	return _SweemBasket.Contract.AccountId(&_SweemBasket.CallOpts)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_SweemBasket *SweemBasketCaller) Allowance(opts *bind.CallOpts, owner common.Address, spender common.Address) (*big.Int, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "allowance", owner, spender)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_SweemBasket *SweemBasketSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _SweemBasket.Contract.Allowance(&_SweemBasket.CallOpts, owner, spender)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_SweemBasket *SweemBasketCallerSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _SweemBasket.Contract.Allowance(&_SweemBasket.CallOpts, owner, spender)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_SweemBasket *SweemBasketCaller) BalanceOf(opts *bind.CallOpts, account common.Address) (*big.Int, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "balanceOf", account)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_SweemBasket *SweemBasketSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _SweemBasket.Contract.BalanceOf(&_SweemBasket.CallOpts, account)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_SweemBasket *SweemBasketCallerSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _SweemBasket.Contract.BalanceOf(&_SweemBasket.CallOpts, account)
}

// Components is a free data retrieval call binding the contract method 0xba62fbe4.
//
// Solidity: function components() view returns((address,uint16)[])
func (_SweemBasket *SweemBasketCaller) Components(opts *bind.CallOpts) ([]Component, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "components")

	if err != nil {
		return *new([]Component), err
	}

	out0 := *abi.ConvertType(out[0], new([]Component)).(*[]Component)

	return out0, err

}

// Components is a free data retrieval call binding the contract method 0xba62fbe4.
//
// Solidity: function components() view returns((address,uint16)[])
func (_SweemBasket *SweemBasketSession) Components() ([]Component, error) {
	return _SweemBasket.Contract.Components(&_SweemBasket.CallOpts)
}

// Components is a free data retrieval call binding the contract method 0xba62fbe4.
//
// Solidity: function components() view returns((address,uint16)[])
func (_SweemBasket *SweemBasketCallerSession) Components() ([]Component, error) {
	return _SweemBasket.Contract.Components(&_SweemBasket.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_SweemBasket *SweemBasketCaller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_SweemBasket *SweemBasketSession) Decimals() (uint8, error) {
	return _SweemBasket.Contract.Decimals(&_SweemBasket.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_SweemBasket *SweemBasketCallerSession) Decimals() (uint8, error) {
	return _SweemBasket.Contract.Decimals(&_SweemBasket.CallOpts)
}

// Factory is a free data retrieval call binding the contract method 0xc45a0155.
//
// Solidity: function factory() view returns(address)
func (_SweemBasket *SweemBasketCaller) Factory(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "factory")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Factory is a free data retrieval call binding the contract method 0xc45a0155.
//
// Solidity: function factory() view returns(address)
func (_SweemBasket *SweemBasketSession) Factory() (common.Address, error) {
	return _SweemBasket.Contract.Factory(&_SweemBasket.CallOpts)
}

// Factory is a free data retrieval call binding the contract method 0xc45a0155.
//
// Solidity: function factory() view returns(address)
func (_SweemBasket *SweemBasketCallerSession) Factory() (common.Address, error) {
	return _SweemBasket.Contract.Factory(&_SweemBasket.CallOpts)
}

// Holdings is a free data retrieval call binding the contract method 0xe79bf13b.
//
// Solidity: function holdings() view returns(uint256[] h)
func (_SweemBasket *SweemBasketCaller) Holdings(opts *bind.CallOpts) ([]*big.Int, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "holdings")

	if err != nil {
		return *new([]*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new([]*big.Int)).(*[]*big.Int)

	return out0, err

}

// Holdings is a free data retrieval call binding the contract method 0xe79bf13b.
//
// Solidity: function holdings() view returns(uint256[] h)
func (_SweemBasket *SweemBasketSession) Holdings() ([]*big.Int, error) {
	return _SweemBasket.Contract.Holdings(&_SweemBasket.CallOpts)
}

// Holdings is a free data retrieval call binding the contract method 0xe79bf13b.
//
// Solidity: function holdings() view returns(uint256[] h)
func (_SweemBasket *SweemBasketCallerSession) Holdings() ([]*big.Int, error) {
	return _SweemBasket.Contract.Holdings(&_SweemBasket.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_SweemBasket *SweemBasketCaller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_SweemBasket *SweemBasketSession) Name() (string, error) {
	return _SweemBasket.Contract.Name(&_SweemBasket.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_SweemBasket *SweemBasketCallerSession) Name() (string, error) {
	return _SweemBasket.Contract.Name(&_SweemBasket.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_SweemBasket *SweemBasketCaller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_SweemBasket *SweemBasketSession) Symbol() (string, error) {
	return _SweemBasket.Contract.Symbol(&_SweemBasket.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_SweemBasket *SweemBasketCallerSession) Symbol() (string, error) {
	return _SweemBasket.Contract.Symbol(&_SweemBasket.CallOpts)
}

// Tokens is a free data retrieval call binding the contract method 0x4f64b2be.
//
// Solidity: function tokens(uint256 ) view returns(address)
func (_SweemBasket *SweemBasketCaller) Tokens(opts *bind.CallOpts, arg0 *big.Int) (common.Address, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "tokens", arg0)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Tokens is a free data retrieval call binding the contract method 0x4f64b2be.
//
// Solidity: function tokens(uint256 ) view returns(address)
func (_SweemBasket *SweemBasketSession) Tokens(arg0 *big.Int) (common.Address, error) {
	return _SweemBasket.Contract.Tokens(&_SweemBasket.CallOpts, arg0)
}

// Tokens is a free data retrieval call binding the contract method 0x4f64b2be.
//
// Solidity: function tokens(uint256 ) view returns(address)
func (_SweemBasket *SweemBasketCallerSession) Tokens(arg0 *big.Int) (common.Address, error) {
	return _SweemBasket.Contract.Tokens(&_SweemBasket.CallOpts, arg0)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_SweemBasket *SweemBasketCaller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_SweemBasket *SweemBasketSession) TotalSupply() (*big.Int, error) {
	return _SweemBasket.Contract.TotalSupply(&_SweemBasket.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_SweemBasket *SweemBasketCallerSession) TotalSupply() (*big.Int, error) {
	return _SweemBasket.Contract.TotalSupply(&_SweemBasket.CallOpts)
}

// Usdc is a free data retrieval call binding the contract method 0x3e413bee.
//
// Solidity: function usdc() view returns(address)
func (_SweemBasket *SweemBasketCaller) Usdc(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SweemBasket.contract.Call(opts, &out, "usdc")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Usdc is a free data retrieval call binding the contract method 0x3e413bee.
//
// Solidity: function usdc() view returns(address)
func (_SweemBasket *SweemBasketSession) Usdc() (common.Address, error) {
	return _SweemBasket.Contract.Usdc(&_SweemBasket.CallOpts)
}

// Usdc is a free data retrieval call binding the contract method 0x3e413bee.
//
// Solidity: function usdc() view returns(address)
func (_SweemBasket *SweemBasketCallerSession) Usdc() (common.Address, error) {
	return _SweemBasket.Contract.Usdc(&_SweemBasket.CallOpts)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketTransactor) Approve(opts *bind.TransactOpts, spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.contract.Transact(opts, "approve", spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketSession) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.Approve(&_SweemBasket.TransactOpts, spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketTransactorSession) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.Approve(&_SweemBasket.TransactOpts, spender, value)
}

// Mint is a paid mutator transaction binding the contract method 0x02acc94b.
//
// Solidity: function mint(uint256 usdcIn, uint256 minShares, uint256 deadline) returns(uint256 shares)
func (_SweemBasket *SweemBasketTransactor) Mint(opts *bind.TransactOpts, usdcIn *big.Int, minShares *big.Int, deadline *big.Int) (*types.Transaction, error) {
	return _SweemBasket.contract.Transact(opts, "mint", usdcIn, minShares, deadline)
}

// Mint is a paid mutator transaction binding the contract method 0x02acc94b.
//
// Solidity: function mint(uint256 usdcIn, uint256 minShares, uint256 deadline) returns(uint256 shares)
func (_SweemBasket *SweemBasketSession) Mint(usdcIn *big.Int, minShares *big.Int, deadline *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.Mint(&_SweemBasket.TransactOpts, usdcIn, minShares, deadline)
}

// Mint is a paid mutator transaction binding the contract method 0x02acc94b.
//
// Solidity: function mint(uint256 usdcIn, uint256 minShares, uint256 deadline) returns(uint256 shares)
func (_SweemBasket *SweemBasketTransactorSession) Mint(usdcIn *big.Int, minShares *big.Int, deadline *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.Mint(&_SweemBasket.TransactOpts, usdcIn, minShares, deadline)
}

// Redeem is a paid mutator transaction binding the contract method 0xb8192205.
//
// Solidity: function redeem(uint256 shares, uint256 minUsdcOut, uint256 deadline) returns(uint256 usdcOut)
func (_SweemBasket *SweemBasketTransactor) Redeem(opts *bind.TransactOpts, shares *big.Int, minUsdcOut *big.Int, deadline *big.Int) (*types.Transaction, error) {
	return _SweemBasket.contract.Transact(opts, "redeem", shares, minUsdcOut, deadline)
}

// Redeem is a paid mutator transaction binding the contract method 0xb8192205.
//
// Solidity: function redeem(uint256 shares, uint256 minUsdcOut, uint256 deadline) returns(uint256 usdcOut)
func (_SweemBasket *SweemBasketSession) Redeem(shares *big.Int, minUsdcOut *big.Int, deadline *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.Redeem(&_SweemBasket.TransactOpts, shares, minUsdcOut, deadline)
}

// Redeem is a paid mutator transaction binding the contract method 0xb8192205.
//
// Solidity: function redeem(uint256 shares, uint256 minUsdcOut, uint256 deadline) returns(uint256 usdcOut)
func (_SweemBasket *SweemBasketTransactorSession) Redeem(shares *big.Int, minUsdcOut *big.Int, deadline *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.Redeem(&_SweemBasket.TransactOpts, shares, minUsdcOut, deadline)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketTransactor) Transfer(opts *bind.TransactOpts, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.contract.Transact(opts, "transfer", to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketSession) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.Transfer(&_SweemBasket.TransactOpts, to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketTransactorSession) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.Transfer(&_SweemBasket.TransactOpts, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketTransactor) TransferFrom(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.contract.Transact(opts, "transferFrom", from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketSession) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.TransferFrom(&_SweemBasket.TransactOpts, from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_SweemBasket *SweemBasketTransactorSession) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _SweemBasket.Contract.TransferFrom(&_SweemBasket.TransactOpts, from, to, value)
}

// SweemBasketApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the SweemBasket contract.
type SweemBasketApprovalIterator struct {
	Event *SweemBasketApproval // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SweemBasketApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SweemBasketApproval)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SweemBasketApproval)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SweemBasketApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SweemBasketApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SweemBasketApproval represents a Approval event raised by the SweemBasket contract.
type SweemBasketApproval struct {
	Owner   common.Address
	Spender common.Address
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_SweemBasket *SweemBasketFilterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*SweemBasketApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _SweemBasket.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &SweemBasketApprovalIterator{contract: _SweemBasket.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_SweemBasket *SweemBasketFilterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *SweemBasketApproval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _SweemBasket.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SweemBasketApproval)
				if err := _SweemBasket.contract.UnpackLog(event, "Approval", log); err != nil {
					// If the signature doesn't match, skip this log.
					if errors.Is(err, bind.ErrEventSignatureMismatch) {
						continue
					}
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseApproval is a log parse operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_SweemBasket *SweemBasketFilterer) ParseApproval(log types.Log) (*SweemBasketApproval, error) {
	event := new(SweemBasketApproval)
	if err := _SweemBasket.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SweemBasketMintedIterator is returned from FilterMinted and is used to iterate over the raw logs and unpacked data for Minted events raised by the SweemBasket contract.
type SweemBasketMintedIterator struct {
	Event *SweemBasketMinted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SweemBasketMintedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SweemBasketMinted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SweemBasketMinted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SweemBasketMintedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SweemBasketMintedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SweemBasketMinted represents a Minted event raised by the SweemBasket contract.
type SweemBasketMinted struct {
	User   common.Address
	UsdcIn *big.Int
	Shares *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterMinted is a free log retrieval operation binding the contract event 0x25b428dfde728ccfaddad7e29e4ac23c24ed7fd1a6e3e3f91894a9a073f5dfff.
//
// Solidity: event Minted(address indexed user, uint256 usdcIn, uint256 shares)
func (_SweemBasket *SweemBasketFilterer) FilterMinted(opts *bind.FilterOpts, user []common.Address) (*SweemBasketMintedIterator, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _SweemBasket.contract.FilterLogs(opts, "Minted", userRule)
	if err != nil {
		return nil, err
	}
	return &SweemBasketMintedIterator{contract: _SweemBasket.contract, event: "Minted", logs: logs, sub: sub}, nil
}

// WatchMinted is a free log subscription operation binding the contract event 0x25b428dfde728ccfaddad7e29e4ac23c24ed7fd1a6e3e3f91894a9a073f5dfff.
//
// Solidity: event Minted(address indexed user, uint256 usdcIn, uint256 shares)
func (_SweemBasket *SweemBasketFilterer) WatchMinted(opts *bind.WatchOpts, sink chan<- *SweemBasketMinted, user []common.Address) (event.Subscription, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _SweemBasket.contract.WatchLogs(opts, "Minted", userRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SweemBasketMinted)
				if err := _SweemBasket.contract.UnpackLog(event, "Minted", log); err != nil {
					// If the signature doesn't match, skip this log.
					if errors.Is(err, bind.ErrEventSignatureMismatch) {
						continue
					}
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseMinted is a log parse operation binding the contract event 0x25b428dfde728ccfaddad7e29e4ac23c24ed7fd1a6e3e3f91894a9a073f5dfff.
//
// Solidity: event Minted(address indexed user, uint256 usdcIn, uint256 shares)
func (_SweemBasket *SweemBasketFilterer) ParseMinted(log types.Log) (*SweemBasketMinted, error) {
	event := new(SweemBasketMinted)
	if err := _SweemBasket.contract.UnpackLog(event, "Minted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SweemBasketRedeemedIterator is returned from FilterRedeemed and is used to iterate over the raw logs and unpacked data for Redeemed events raised by the SweemBasket contract.
type SweemBasketRedeemedIterator struct {
	Event *SweemBasketRedeemed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SweemBasketRedeemedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SweemBasketRedeemed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SweemBasketRedeemed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SweemBasketRedeemedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SweemBasketRedeemedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SweemBasketRedeemed represents a Redeemed event raised by the SweemBasket contract.
type SweemBasketRedeemed struct {
	User    common.Address
	Shares  *big.Int
	UsdcOut *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRedeemed is a free log retrieval operation binding the contract event 0xf3a670cd3af7d64b488926880889d08a8585a138ff455227af6737339a1ec262.
//
// Solidity: event Redeemed(address indexed user, uint256 shares, uint256 usdcOut)
func (_SweemBasket *SweemBasketFilterer) FilterRedeemed(opts *bind.FilterOpts, user []common.Address) (*SweemBasketRedeemedIterator, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _SweemBasket.contract.FilterLogs(opts, "Redeemed", userRule)
	if err != nil {
		return nil, err
	}
	return &SweemBasketRedeemedIterator{contract: _SweemBasket.contract, event: "Redeemed", logs: logs, sub: sub}, nil
}

// WatchRedeemed is a free log subscription operation binding the contract event 0xf3a670cd3af7d64b488926880889d08a8585a138ff455227af6737339a1ec262.
//
// Solidity: event Redeemed(address indexed user, uint256 shares, uint256 usdcOut)
func (_SweemBasket *SweemBasketFilterer) WatchRedeemed(opts *bind.WatchOpts, sink chan<- *SweemBasketRedeemed, user []common.Address) (event.Subscription, error) {

	var userRule []interface{}
	for _, userItem := range user {
		userRule = append(userRule, userItem)
	}

	logs, sub, err := _SweemBasket.contract.WatchLogs(opts, "Redeemed", userRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SweemBasketRedeemed)
				if err := _SweemBasket.contract.UnpackLog(event, "Redeemed", log); err != nil {
					// If the signature doesn't match, skip this log.
					if errors.Is(err, bind.ErrEventSignatureMismatch) {
						continue
					}
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRedeemed is a log parse operation binding the contract event 0xf3a670cd3af7d64b488926880889d08a8585a138ff455227af6737339a1ec262.
//
// Solidity: event Redeemed(address indexed user, uint256 shares, uint256 usdcOut)
func (_SweemBasket *SweemBasketFilterer) ParseRedeemed(log types.Log) (*SweemBasketRedeemed, error) {
	event := new(SweemBasketRedeemed)
	if err := _SweemBasket.contract.UnpackLog(event, "Redeemed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SweemBasketTransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the SweemBasket contract.
type SweemBasketTransferIterator struct {
	Event *SweemBasketTransfer // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *SweemBasketTransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SweemBasketTransfer)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(SweemBasketTransfer)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *SweemBasketTransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SweemBasketTransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SweemBasketTransfer represents a Transfer event raised by the SweemBasket contract.
type SweemBasketTransfer struct {
	From  common.Address
	To    common.Address
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_SweemBasket *SweemBasketFilterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*SweemBasketTransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _SweemBasket.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &SweemBasketTransferIterator{contract: _SweemBasket.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_SweemBasket *SweemBasketFilterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *SweemBasketTransfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _SweemBasket.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SweemBasketTransfer)
				if err := _SweemBasket.contract.UnpackLog(event, "Transfer", log); err != nil {
					// If the signature doesn't match, skip this log.
					if errors.Is(err, bind.ErrEventSignatureMismatch) {
						continue
					}
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTransfer is a log parse operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_SweemBasket *SweemBasketFilterer) ParseTransfer(log types.Log) (*SweemBasketTransfer, error) {
	event := new(SweemBasketTransfer)
	if err := _SweemBasket.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
