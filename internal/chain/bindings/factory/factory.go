// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package factory

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

// SweemBasketFactoryMetaData contains all meta data concerning the SweemBasketFactory contract.
var SweemBasketFactoryMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"accountCore_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"usdc_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"owner_\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"accountCore\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"allowedMarkets\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address[]\",\"internalType\":\"address[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"baskets\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address[]\",\"internalType\":\"address[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"createBasket\",\"inputs\":[{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"symbol\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"comps\",\"type\":\"tuple[]\",\"internalType\":\"structComponent[]\",\"components\":[{\"name\":\"market\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"weightBps\",\"type\":\"uint16\",\"internalType\":\"uint16\"}]}],\"outputs\":[{\"name\":\"basket\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"isBasket\",\"inputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"isMarketAllowed\",\"inputs\":[{\"name\":\"market\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"owner\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"paused\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"renounceOwnership\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setMarketAllowed\",\"inputs\":[{\"name\":\"market\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"allowed\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setPaused\",\"inputs\":[{\"name\":\"p\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"transferOwnership\",\"inputs\":[{\"name\":\"newOwner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"usdc\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"event\",\"name\":\"BasketCreated\",\"inputs\":[{\"name\":\"basket\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"name\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"symbol\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"components\",\"type\":\"tuple[]\",\"indexed\":false,\"internalType\":\"structComponent[]\",\"components\":[{\"name\":\"market\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"weightBps\",\"type\":\"uint16\",\"internalType\":\"uint16\"}]}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"MarketAllowed\",\"inputs\":[{\"name\":\"market\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"allowed\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OwnershipTransferred\",\"inputs\":[{\"name\":\"previousOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PausedSet\",\"inputs\":[{\"name\":\"paused\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"OwnableInvalidOwner\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OwnableUnauthorizedAccount\",\"inputs\":[{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"}]}]",
}

// SweemBasketFactoryABI is the input ABI used to generate the binding from.
// Deprecated: Use SweemBasketFactoryMetaData.ABI instead.
var SweemBasketFactoryABI = SweemBasketFactoryMetaData.ABI

// SweemBasketFactory is an auto generated Go binding around an Ethereum contract.
type SweemBasketFactory struct {
	SweemBasketFactoryCaller     // Read-only binding to the contract
	SweemBasketFactoryTransactor // Write-only binding to the contract
	SweemBasketFactoryFilterer   // Log filterer for contract events
}

// SweemBasketFactoryCaller is an auto generated read-only Go binding around an Ethereum contract.
type SweemBasketFactoryCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SweemBasketFactoryTransactor is an auto generated write-only Go binding around an Ethereum contract.
type SweemBasketFactoryTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SweemBasketFactoryFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type SweemBasketFactoryFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SweemBasketFactorySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type SweemBasketFactorySession struct {
	Contract     *SweemBasketFactory // Generic contract binding to set the session for
	CallOpts     bind.CallOpts       // Call options to use throughout this session
	TransactOpts bind.TransactOpts   // Transaction auth options to use throughout this session
}

// SweemBasketFactoryCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type SweemBasketFactoryCallerSession struct {
	Contract *SweemBasketFactoryCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts             // Call options to use throughout this session
}

// SweemBasketFactoryTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type SweemBasketFactoryTransactorSession struct {
	Contract     *SweemBasketFactoryTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts             // Transaction auth options to use throughout this session
}

// SweemBasketFactoryRaw is an auto generated low-level Go binding around an Ethereum contract.
type SweemBasketFactoryRaw struct {
	Contract *SweemBasketFactory // Generic contract binding to access the raw methods on
}

// SweemBasketFactoryCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type SweemBasketFactoryCallerRaw struct {
	Contract *SweemBasketFactoryCaller // Generic read-only contract binding to access the raw methods on
}

// SweemBasketFactoryTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type SweemBasketFactoryTransactorRaw struct {
	Contract *SweemBasketFactoryTransactor // Generic write-only contract binding to access the raw methods on
}

// NewSweemBasketFactory creates a new instance of SweemBasketFactory, bound to a specific deployed contract.
func NewSweemBasketFactory(address common.Address, backend bind.ContractBackend) (*SweemBasketFactory, error) {
	contract, err := bindSweemBasketFactory(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &SweemBasketFactory{SweemBasketFactoryCaller: SweemBasketFactoryCaller{contract: contract}, SweemBasketFactoryTransactor: SweemBasketFactoryTransactor{contract: contract}, SweemBasketFactoryFilterer: SweemBasketFactoryFilterer{contract: contract}}, nil
}

// NewSweemBasketFactoryCaller creates a new read-only instance of SweemBasketFactory, bound to a specific deployed contract.
func NewSweemBasketFactoryCaller(address common.Address, caller bind.ContractCaller) (*SweemBasketFactoryCaller, error) {
	contract, err := bindSweemBasketFactory(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &SweemBasketFactoryCaller{contract: contract}, nil
}

// NewSweemBasketFactoryTransactor creates a new write-only instance of SweemBasketFactory, bound to a specific deployed contract.
func NewSweemBasketFactoryTransactor(address common.Address, transactor bind.ContractTransactor) (*SweemBasketFactoryTransactor, error) {
	contract, err := bindSweemBasketFactory(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &SweemBasketFactoryTransactor{contract: contract}, nil
}

// NewSweemBasketFactoryFilterer creates a new log filterer instance of SweemBasketFactory, bound to a specific deployed contract.
func NewSweemBasketFactoryFilterer(address common.Address, filterer bind.ContractFilterer) (*SweemBasketFactoryFilterer, error) {
	contract, err := bindSweemBasketFactory(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &SweemBasketFactoryFilterer{contract: contract}, nil
}

// bindSweemBasketFactory binds a generic wrapper to an already deployed contract.
func bindSweemBasketFactory(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := SweemBasketFactoryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SweemBasketFactory *SweemBasketFactoryRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SweemBasketFactory.Contract.SweemBasketFactoryCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SweemBasketFactory *SweemBasketFactoryRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.SweemBasketFactoryTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SweemBasketFactory *SweemBasketFactoryRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.SweemBasketFactoryTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SweemBasketFactory *SweemBasketFactoryCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SweemBasketFactory.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SweemBasketFactory *SweemBasketFactoryTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SweemBasketFactory *SweemBasketFactoryTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.contract.Transact(opts, method, params...)
}

// AccountCore is a free data retrieval call binding the contract method 0x15b5b207.
//
// Solidity: function accountCore() view returns(address)
func (_SweemBasketFactory *SweemBasketFactoryCaller) AccountCore(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SweemBasketFactory.contract.Call(opts, &out, "accountCore")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// AccountCore is a free data retrieval call binding the contract method 0x15b5b207.
//
// Solidity: function accountCore() view returns(address)
func (_SweemBasketFactory *SweemBasketFactorySession) AccountCore() (common.Address, error) {
	return _SweemBasketFactory.Contract.AccountCore(&_SweemBasketFactory.CallOpts)
}

// AccountCore is a free data retrieval call binding the contract method 0x15b5b207.
//
// Solidity: function accountCore() view returns(address)
func (_SweemBasketFactory *SweemBasketFactoryCallerSession) AccountCore() (common.Address, error) {
	return _SweemBasketFactory.Contract.AccountCore(&_SweemBasketFactory.CallOpts)
}

// AllowedMarkets is a free data retrieval call binding the contract method 0xd5b31ab9.
//
// Solidity: function allowedMarkets() view returns(address[])
func (_SweemBasketFactory *SweemBasketFactoryCaller) AllowedMarkets(opts *bind.CallOpts) ([]common.Address, error) {
	var out []interface{}
	err := _SweemBasketFactory.contract.Call(opts, &out, "allowedMarkets")

	if err != nil {
		return *new([]common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new([]common.Address)).(*[]common.Address)

	return out0, err

}

// AllowedMarkets is a free data retrieval call binding the contract method 0xd5b31ab9.
//
// Solidity: function allowedMarkets() view returns(address[])
func (_SweemBasketFactory *SweemBasketFactorySession) AllowedMarkets() ([]common.Address, error) {
	return _SweemBasketFactory.Contract.AllowedMarkets(&_SweemBasketFactory.CallOpts)
}

// AllowedMarkets is a free data retrieval call binding the contract method 0xd5b31ab9.
//
// Solidity: function allowedMarkets() view returns(address[])
func (_SweemBasketFactory *SweemBasketFactoryCallerSession) AllowedMarkets() ([]common.Address, error) {
	return _SweemBasketFactory.Contract.AllowedMarkets(&_SweemBasketFactory.CallOpts)
}

// Baskets is a free data retrieval call binding the contract method 0xdae105f1.
//
// Solidity: function baskets() view returns(address[])
func (_SweemBasketFactory *SweemBasketFactoryCaller) Baskets(opts *bind.CallOpts) ([]common.Address, error) {
	var out []interface{}
	err := _SweemBasketFactory.contract.Call(opts, &out, "baskets")

	if err != nil {
		return *new([]common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new([]common.Address)).(*[]common.Address)

	return out0, err

}

// Baskets is a free data retrieval call binding the contract method 0xdae105f1.
//
// Solidity: function baskets() view returns(address[])
func (_SweemBasketFactory *SweemBasketFactorySession) Baskets() ([]common.Address, error) {
	return _SweemBasketFactory.Contract.Baskets(&_SweemBasketFactory.CallOpts)
}

// Baskets is a free data retrieval call binding the contract method 0xdae105f1.
//
// Solidity: function baskets() view returns(address[])
func (_SweemBasketFactory *SweemBasketFactoryCallerSession) Baskets() ([]common.Address, error) {
	return _SweemBasketFactory.Contract.Baskets(&_SweemBasketFactory.CallOpts)
}

// IsBasket is a free data retrieval call binding the contract method 0xeb44d482.
//
// Solidity: function isBasket(address ) view returns(bool)
func (_SweemBasketFactory *SweemBasketFactoryCaller) IsBasket(opts *bind.CallOpts, arg0 common.Address) (bool, error) {
	var out []interface{}
	err := _SweemBasketFactory.contract.Call(opts, &out, "isBasket", arg0)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsBasket is a free data retrieval call binding the contract method 0xeb44d482.
//
// Solidity: function isBasket(address ) view returns(bool)
func (_SweemBasketFactory *SweemBasketFactorySession) IsBasket(arg0 common.Address) (bool, error) {
	return _SweemBasketFactory.Contract.IsBasket(&_SweemBasketFactory.CallOpts, arg0)
}

// IsBasket is a free data retrieval call binding the contract method 0xeb44d482.
//
// Solidity: function isBasket(address ) view returns(bool)
func (_SweemBasketFactory *SweemBasketFactoryCallerSession) IsBasket(arg0 common.Address) (bool, error) {
	return _SweemBasketFactory.Contract.IsBasket(&_SweemBasketFactory.CallOpts, arg0)
}

// IsMarketAllowed is a free data retrieval call binding the contract method 0xe72e4363.
//
// Solidity: function isMarketAllowed(address market) view returns(bool)
func (_SweemBasketFactory *SweemBasketFactoryCaller) IsMarketAllowed(opts *bind.CallOpts, market common.Address) (bool, error) {
	var out []interface{}
	err := _SweemBasketFactory.contract.Call(opts, &out, "isMarketAllowed", market)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsMarketAllowed is a free data retrieval call binding the contract method 0xe72e4363.
//
// Solidity: function isMarketAllowed(address market) view returns(bool)
func (_SweemBasketFactory *SweemBasketFactorySession) IsMarketAllowed(market common.Address) (bool, error) {
	return _SweemBasketFactory.Contract.IsMarketAllowed(&_SweemBasketFactory.CallOpts, market)
}

// IsMarketAllowed is a free data retrieval call binding the contract method 0xe72e4363.
//
// Solidity: function isMarketAllowed(address market) view returns(bool)
func (_SweemBasketFactory *SweemBasketFactoryCallerSession) IsMarketAllowed(market common.Address) (bool, error) {
	return _SweemBasketFactory.Contract.IsMarketAllowed(&_SweemBasketFactory.CallOpts, market)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_SweemBasketFactory *SweemBasketFactoryCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SweemBasketFactory.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_SweemBasketFactory *SweemBasketFactorySession) Owner() (common.Address, error) {
	return _SweemBasketFactory.Contract.Owner(&_SweemBasketFactory.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_SweemBasketFactory *SweemBasketFactoryCallerSession) Owner() (common.Address, error) {
	return _SweemBasketFactory.Contract.Owner(&_SweemBasketFactory.CallOpts)
}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_SweemBasketFactory *SweemBasketFactoryCaller) Paused(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _SweemBasketFactory.contract.Call(opts, &out, "paused")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_SweemBasketFactory *SweemBasketFactorySession) Paused() (bool, error) {
	return _SweemBasketFactory.Contract.Paused(&_SweemBasketFactory.CallOpts)
}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_SweemBasketFactory *SweemBasketFactoryCallerSession) Paused() (bool, error) {
	return _SweemBasketFactory.Contract.Paused(&_SweemBasketFactory.CallOpts)
}

// Usdc is a free data retrieval call binding the contract method 0x3e413bee.
//
// Solidity: function usdc() view returns(address)
func (_SweemBasketFactory *SweemBasketFactoryCaller) Usdc(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SweemBasketFactory.contract.Call(opts, &out, "usdc")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Usdc is a free data retrieval call binding the contract method 0x3e413bee.
//
// Solidity: function usdc() view returns(address)
func (_SweemBasketFactory *SweemBasketFactorySession) Usdc() (common.Address, error) {
	return _SweemBasketFactory.Contract.Usdc(&_SweemBasketFactory.CallOpts)
}

// Usdc is a free data retrieval call binding the contract method 0x3e413bee.
//
// Solidity: function usdc() view returns(address)
func (_SweemBasketFactory *SweemBasketFactoryCallerSession) Usdc() (common.Address, error) {
	return _SweemBasketFactory.Contract.Usdc(&_SweemBasketFactory.CallOpts)
}

// CreateBasket is a paid mutator transaction binding the contract method 0x44f02c82.
//
// Solidity: function createBasket(string name, string symbol, (address,uint16)[] comps) returns(address basket)
func (_SweemBasketFactory *SweemBasketFactoryTransactor) CreateBasket(opts *bind.TransactOpts, name string, symbol string, comps []Component) (*types.Transaction, error) {
	return _SweemBasketFactory.contract.Transact(opts, "createBasket", name, symbol, comps)
}

// CreateBasket is a paid mutator transaction binding the contract method 0x44f02c82.
//
// Solidity: function createBasket(string name, string symbol, (address,uint16)[] comps) returns(address basket)
func (_SweemBasketFactory *SweemBasketFactorySession) CreateBasket(name string, symbol string, comps []Component) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.CreateBasket(&_SweemBasketFactory.TransactOpts, name, symbol, comps)
}

// CreateBasket is a paid mutator transaction binding the contract method 0x44f02c82.
//
// Solidity: function createBasket(string name, string symbol, (address,uint16)[] comps) returns(address basket)
func (_SweemBasketFactory *SweemBasketFactoryTransactorSession) CreateBasket(name string, symbol string, comps []Component) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.CreateBasket(&_SweemBasketFactory.TransactOpts, name, symbol, comps)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_SweemBasketFactory *SweemBasketFactoryTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SweemBasketFactory.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_SweemBasketFactory *SweemBasketFactorySession) RenounceOwnership() (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.RenounceOwnership(&_SweemBasketFactory.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_SweemBasketFactory *SweemBasketFactoryTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.RenounceOwnership(&_SweemBasketFactory.TransactOpts)
}

// SetMarketAllowed is a paid mutator transaction binding the contract method 0xd1c921a2.
//
// Solidity: function setMarketAllowed(address market, bool allowed) returns()
func (_SweemBasketFactory *SweemBasketFactoryTransactor) SetMarketAllowed(opts *bind.TransactOpts, market common.Address, allowed bool) (*types.Transaction, error) {
	return _SweemBasketFactory.contract.Transact(opts, "setMarketAllowed", market, allowed)
}

// SetMarketAllowed is a paid mutator transaction binding the contract method 0xd1c921a2.
//
// Solidity: function setMarketAllowed(address market, bool allowed) returns()
func (_SweemBasketFactory *SweemBasketFactorySession) SetMarketAllowed(market common.Address, allowed bool) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.SetMarketAllowed(&_SweemBasketFactory.TransactOpts, market, allowed)
}

// SetMarketAllowed is a paid mutator transaction binding the contract method 0xd1c921a2.
//
// Solidity: function setMarketAllowed(address market, bool allowed) returns()
func (_SweemBasketFactory *SweemBasketFactoryTransactorSession) SetMarketAllowed(market common.Address, allowed bool) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.SetMarketAllowed(&_SweemBasketFactory.TransactOpts, market, allowed)
}

// SetPaused is a paid mutator transaction binding the contract method 0x16c38b3c.
//
// Solidity: function setPaused(bool p) returns()
func (_SweemBasketFactory *SweemBasketFactoryTransactor) SetPaused(opts *bind.TransactOpts, p bool) (*types.Transaction, error) {
	return _SweemBasketFactory.contract.Transact(opts, "setPaused", p)
}

// SetPaused is a paid mutator transaction binding the contract method 0x16c38b3c.
//
// Solidity: function setPaused(bool p) returns()
func (_SweemBasketFactory *SweemBasketFactorySession) SetPaused(p bool) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.SetPaused(&_SweemBasketFactory.TransactOpts, p)
}

// SetPaused is a paid mutator transaction binding the contract method 0x16c38b3c.
//
// Solidity: function setPaused(bool p) returns()
func (_SweemBasketFactory *SweemBasketFactoryTransactorSession) SetPaused(p bool) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.SetPaused(&_SweemBasketFactory.TransactOpts, p)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_SweemBasketFactory *SweemBasketFactoryTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _SweemBasketFactory.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_SweemBasketFactory *SweemBasketFactorySession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.TransferOwnership(&_SweemBasketFactory.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_SweemBasketFactory *SweemBasketFactoryTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _SweemBasketFactory.Contract.TransferOwnership(&_SweemBasketFactory.TransactOpts, newOwner)
}

// SweemBasketFactoryBasketCreatedIterator is returned from FilterBasketCreated and is used to iterate over the raw logs and unpacked data for BasketCreated events raised by the SweemBasketFactory contract.
type SweemBasketFactoryBasketCreatedIterator struct {
	Event *SweemBasketFactoryBasketCreated // Event containing the contract specifics and raw log

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
func (it *SweemBasketFactoryBasketCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SweemBasketFactoryBasketCreated)
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
		it.Event = new(SweemBasketFactoryBasketCreated)
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
func (it *SweemBasketFactoryBasketCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SweemBasketFactoryBasketCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SweemBasketFactoryBasketCreated represents a BasketCreated event raised by the SweemBasketFactory contract.
type SweemBasketFactoryBasketCreated struct {
	Basket     common.Address
	Name       string
	Symbol     string
	Components []Component
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterBasketCreated is a free log retrieval operation binding the contract event 0xf0bebdaea16b0826d27cd0e0bf445b3d97b2d45cf5fda8bb3891916c2f4f4117.
//
// Solidity: event BasketCreated(address indexed basket, string name, string symbol, (address,uint16)[] components)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) FilterBasketCreated(opts *bind.FilterOpts, basket []common.Address) (*SweemBasketFactoryBasketCreatedIterator, error) {

	var basketRule []interface{}
	for _, basketItem := range basket {
		basketRule = append(basketRule, basketItem)
	}

	logs, sub, err := _SweemBasketFactory.contract.FilterLogs(opts, "BasketCreated", basketRule)
	if err != nil {
		return nil, err
	}
	return &SweemBasketFactoryBasketCreatedIterator{contract: _SweemBasketFactory.contract, event: "BasketCreated", logs: logs, sub: sub}, nil
}

// WatchBasketCreated is a free log subscription operation binding the contract event 0xf0bebdaea16b0826d27cd0e0bf445b3d97b2d45cf5fda8bb3891916c2f4f4117.
//
// Solidity: event BasketCreated(address indexed basket, string name, string symbol, (address,uint16)[] components)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) WatchBasketCreated(opts *bind.WatchOpts, sink chan<- *SweemBasketFactoryBasketCreated, basket []common.Address) (event.Subscription, error) {

	var basketRule []interface{}
	for _, basketItem := range basket {
		basketRule = append(basketRule, basketItem)
	}

	logs, sub, err := _SweemBasketFactory.contract.WatchLogs(opts, "BasketCreated", basketRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SweemBasketFactoryBasketCreated)
				if err := _SweemBasketFactory.contract.UnpackLog(event, "BasketCreated", log); err != nil {
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

// ParseBasketCreated is a log parse operation binding the contract event 0xf0bebdaea16b0826d27cd0e0bf445b3d97b2d45cf5fda8bb3891916c2f4f4117.
//
// Solidity: event BasketCreated(address indexed basket, string name, string symbol, (address,uint16)[] components)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) ParseBasketCreated(log types.Log) (*SweemBasketFactoryBasketCreated, error) {
	event := new(SweemBasketFactoryBasketCreated)
	if err := _SweemBasketFactory.contract.UnpackLog(event, "BasketCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SweemBasketFactoryMarketAllowedIterator is returned from FilterMarketAllowed and is used to iterate over the raw logs and unpacked data for MarketAllowed events raised by the SweemBasketFactory contract.
type SweemBasketFactoryMarketAllowedIterator struct {
	Event *SweemBasketFactoryMarketAllowed // Event containing the contract specifics and raw log

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
func (it *SweemBasketFactoryMarketAllowedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SweemBasketFactoryMarketAllowed)
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
		it.Event = new(SweemBasketFactoryMarketAllowed)
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
func (it *SweemBasketFactoryMarketAllowedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SweemBasketFactoryMarketAllowedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SweemBasketFactoryMarketAllowed represents a MarketAllowed event raised by the SweemBasketFactory contract.
type SweemBasketFactoryMarketAllowed struct {
	Market  common.Address
	Allowed bool
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterMarketAllowed is a free log retrieval operation binding the contract event 0xfc76eefd3ca0046c0147b7b22cb8be1b5518a2fabbf5e198afd37f8136923a3b.
//
// Solidity: event MarketAllowed(address indexed market, bool allowed)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) FilterMarketAllowed(opts *bind.FilterOpts, market []common.Address) (*SweemBasketFactoryMarketAllowedIterator, error) {

	var marketRule []interface{}
	for _, marketItem := range market {
		marketRule = append(marketRule, marketItem)
	}

	logs, sub, err := _SweemBasketFactory.contract.FilterLogs(opts, "MarketAllowed", marketRule)
	if err != nil {
		return nil, err
	}
	return &SweemBasketFactoryMarketAllowedIterator{contract: _SweemBasketFactory.contract, event: "MarketAllowed", logs: logs, sub: sub}, nil
}

// WatchMarketAllowed is a free log subscription operation binding the contract event 0xfc76eefd3ca0046c0147b7b22cb8be1b5518a2fabbf5e198afd37f8136923a3b.
//
// Solidity: event MarketAllowed(address indexed market, bool allowed)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) WatchMarketAllowed(opts *bind.WatchOpts, sink chan<- *SweemBasketFactoryMarketAllowed, market []common.Address) (event.Subscription, error) {

	var marketRule []interface{}
	for _, marketItem := range market {
		marketRule = append(marketRule, marketItem)
	}

	logs, sub, err := _SweemBasketFactory.contract.WatchLogs(opts, "MarketAllowed", marketRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SweemBasketFactoryMarketAllowed)
				if err := _SweemBasketFactory.contract.UnpackLog(event, "MarketAllowed", log); err != nil {
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

// ParseMarketAllowed is a log parse operation binding the contract event 0xfc76eefd3ca0046c0147b7b22cb8be1b5518a2fabbf5e198afd37f8136923a3b.
//
// Solidity: event MarketAllowed(address indexed market, bool allowed)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) ParseMarketAllowed(log types.Log) (*SweemBasketFactoryMarketAllowed, error) {
	event := new(SweemBasketFactoryMarketAllowed)
	if err := _SweemBasketFactory.contract.UnpackLog(event, "MarketAllowed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SweemBasketFactoryOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the SweemBasketFactory contract.
type SweemBasketFactoryOwnershipTransferredIterator struct {
	Event *SweemBasketFactoryOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *SweemBasketFactoryOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SweemBasketFactoryOwnershipTransferred)
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
		it.Event = new(SweemBasketFactoryOwnershipTransferred)
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
func (it *SweemBasketFactoryOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SweemBasketFactoryOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SweemBasketFactoryOwnershipTransferred represents a OwnershipTransferred event raised by the SweemBasketFactory contract.
type SweemBasketFactoryOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*SweemBasketFactoryOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _SweemBasketFactory.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &SweemBasketFactoryOwnershipTransferredIterator{contract: _SweemBasketFactory.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *SweemBasketFactoryOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _SweemBasketFactory.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SweemBasketFactoryOwnershipTransferred)
				if err := _SweemBasketFactory.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) ParseOwnershipTransferred(log types.Log) (*SweemBasketFactoryOwnershipTransferred, error) {
	event := new(SweemBasketFactoryOwnershipTransferred)
	if err := _SweemBasketFactory.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SweemBasketFactoryPausedSetIterator is returned from FilterPausedSet and is used to iterate over the raw logs and unpacked data for PausedSet events raised by the SweemBasketFactory contract.
type SweemBasketFactoryPausedSetIterator struct {
	Event *SweemBasketFactoryPausedSet // Event containing the contract specifics and raw log

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
func (it *SweemBasketFactoryPausedSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SweemBasketFactoryPausedSet)
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
		it.Event = new(SweemBasketFactoryPausedSet)
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
func (it *SweemBasketFactoryPausedSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SweemBasketFactoryPausedSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SweemBasketFactoryPausedSet represents a PausedSet event raised by the SweemBasketFactory contract.
type SweemBasketFactoryPausedSet struct {
	Paused bool
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterPausedSet is a free log retrieval operation binding the contract event 0x40db37ff5c0bdc2c427fbb2078c8f24afea940abac0e3c23bb4ea3bf2da2b212.
//
// Solidity: event PausedSet(bool paused)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) FilterPausedSet(opts *bind.FilterOpts) (*SweemBasketFactoryPausedSetIterator, error) {

	logs, sub, err := _SweemBasketFactory.contract.FilterLogs(opts, "PausedSet")
	if err != nil {
		return nil, err
	}
	return &SweemBasketFactoryPausedSetIterator{contract: _SweemBasketFactory.contract, event: "PausedSet", logs: logs, sub: sub}, nil
}

// WatchPausedSet is a free log subscription operation binding the contract event 0x40db37ff5c0bdc2c427fbb2078c8f24afea940abac0e3c23bb4ea3bf2da2b212.
//
// Solidity: event PausedSet(bool paused)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) WatchPausedSet(opts *bind.WatchOpts, sink chan<- *SweemBasketFactoryPausedSet) (event.Subscription, error) {

	logs, sub, err := _SweemBasketFactory.contract.WatchLogs(opts, "PausedSet")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SweemBasketFactoryPausedSet)
				if err := _SweemBasketFactory.contract.UnpackLog(event, "PausedSet", log); err != nil {
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

// ParsePausedSet is a log parse operation binding the contract event 0x40db37ff5c0bdc2c427fbb2078c8f24afea940abac0e3c23bb4ea3bf2da2b212.
//
// Solidity: event PausedSet(bool paused)
func (_SweemBasketFactory *SweemBasketFactoryFilterer) ParsePausedSet(log types.Log) (*SweemBasketFactoryPausedSet, error) {
	event := new(SweemBasketFactoryPausedSet)
	if err := _SweemBasketFactory.contract.UnpackLog(event, "PausedSet", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
