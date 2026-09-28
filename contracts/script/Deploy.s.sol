// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import "forge-std/Script.sol";
import {SweemBasketFactory} from "../src/SweemBasketFactory.sol";
import {Component} from "../src/SweemBasket.sol";

contract Deploy is Script {
    address constant ACCOUNT_CORE = 0x6384e9b2Bf3b65e1535403a0A543b5FDA905eE22;
    address constant USDC = 0xEe0722ead54f1B4fe97bE399Be43BC0226a6f97E;
    address constant MON_USDC = 0xfdbE356828c8f5A5d5ed4f69ddE0816f4058Ef61;
    address constant WETH_USDC = 0xa9C2936656a7D2143720BcD91Ba8506200B7CbE7;
    address constant CBBTC_USDC = 0x5BDEA6F9F9abA34F4EcB9B865646A792b835ef7f;
    address constant XAUT_USDC = 0x0B4dD2A7b09d5c5401149fFe51301Cc589017343;

    function run() external returns (SweemBasketFactory factory, address blue) {
        uint256 pk = vm.envUint("PRIVATE_KEY");
        vm.startBroadcast(pk);
        factory = new SweemBasketFactory(ACCOUNT_CORE, USDC, vm.addr(pk));
        factory.setMarketAllowed(MON_USDC, true);
        factory.setMarketAllowed(WETH_USDC, true);
        factory.setMarketAllowed(CBBTC_USDC, true);
        factory.setMarketAllowed(XAUT_USDC, true);
        Component[] memory c = new Component[](3);
        c[0] = Component(MON_USDC, 4000);
        c[1] = Component(WETH_USDC, 3000);
        c[2] = Component(CBBTC_USDC, 3000);
        blue = factory.createBasket("sweem Blue", "BLUE", c);
        vm.stopBroadcast();
    }
}
