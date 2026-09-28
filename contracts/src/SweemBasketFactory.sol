// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {EnumerableSet} from "@openzeppelin/contracts/utils/structs/EnumerableSet.sol";
import {SweemBasket, Component} from "./SweemBasket.sol";

contract SweemBasketFactory is Ownable {
    using EnumerableSet for EnumerableSet.AddressSet;

    event MarketAllowed(address indexed market, bool allowed);
    event PausedSet(bool paused);
    event BasketCreated(address indexed basket, string name, string symbol, Component[] components);

    address public immutable accountCore;
    address public immutable usdc;
    bool public paused;
    mapping(address => bool) public isBasket;

    EnumerableSet.AddressSet internal _allowed;
    address[] internal _baskets;

    constructor(address accountCore_, address usdc_, address owner_) Ownable(owner_) {
        accountCore = accountCore_;
        usdc = usdc_;
    }

    function setMarketAllowed(address market, bool allowed) external onlyOwner {
        allowed ? _allowed.add(market) : _allowed.remove(market);
        emit MarketAllowed(market, allowed);
    }

    function setPaused(bool p) external onlyOwner {
        paused = p;
        emit PausedSet(p);
    }

    /// Validation (allowlist, USDC quote, weights, dupes) runs in the basket constructor.
    function createBasket(string calldata name, string calldata symbol, Component[] calldata comps)
        external
        returns (address basket)
    {
        basket = address(new SweemBasket(name, symbol, comps));
        isBasket[basket] = true;
        _baskets.push(basket);
        emit BasketCreated(basket, name, symbol, comps);
    }

    function isMarketAllowed(address market) external view returns (bool) {
        return _allowed.contains(market);
    }

    function allowedMarkets() external view returns (address[] memory) {
        return _allowed.values();
    }

    function baskets() external view returns (address[] memory) {
        return _baskets;
    }
}
