// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";

interface IERC20 {
    function approve(address, uint256) external returns (bool);
    function balanceOf(address) external view returns (uint256);
}

interface IOrderBook {
    function placeAndExecuteMarketBuy(uint96 quoteSize, uint256 minAmountOut, bool isMargin, bool isFillOrKill)
        external payable returns (uint256);
    function placeAndExecuteMarketSell(uint96 size, uint256 minAmountOut, bool isMargin, bool isFillOrKill)
        external payable returns (uint256);
}

// Contract trader (non-EOA). Native MON base -> needs receive().
contract Trader {
    IOrderBook constant OB = IOrderBook(0x065C9d28E428A0db40191a54d33d5b7c71a9C394); // MON-USDC
    IERC20 constant USDC = IERC20(0x754704Bc059F8C67012fEd69BC8A327a5aafb603);

    // quoteSize in pricePrecision units (1e8 = 1 USDC)
    function buy(uint96 quoteSize) external returns (uint256) {
        USDC.approve(address(OB), type(uint256).max);
        return OB.placeAndExecuteMarketBuy(quoteSize, 0, false, false);
    }

    // size in sizePrecision units (1e10 = 1 MON); msg.value = size in wei
    function sell(uint96 size, uint256 valueWei) external returns (uint256) {
        return OB.placeAndExecuteMarketSell{value: valueWei}(size, 0, false, false);
    }

    receive() external payable {}
}

contract KuruMarketTest is Test {
    IERC20 constant USDC = IERC20(0x754704Bc059F8C67012fEd69BC8A327a5aafb603);
    Trader t;

    function setUp() public {
        vm.createSelectFork("https://rpc.monad.xyz");
        t = new Trader();
        deal(address(USDC), address(t), 10e6); // 10 USDC
    }

    function test_buyThenSell() public {
        uint256 u0 = USDC.balanceOf(address(t));
        uint256 m0 = address(t).balance;

        uint256 g = gasleft();
        t.buy(10e8); // 10 USDC
        uint256 gasBuy = g - gasleft();
        uint256 monGot = address(t).balance - m0;
        uint256 usdcSpent = u0 - USDC.balanceOf(address(t));
        emit log_named_uint("buy: USDC spent (6dp)", usdcSpent);
        emit log_named_uint("buy: MON recv (wei)", monGot);
        emit log_named_uint("buy: gas", gasBuy);
        assertGt(monGot, 0);

        // sell back, rounded down to sizePrecision (1e8 wei granularity)
        uint256 sizeUnits = monGot / 1e8;
        uint256 valueWei = sizeUnits * 1e8;
        uint256 u1 = USDC.balanceOf(address(t));
        g = gasleft();
        t.sell(uint96(sizeUnits), valueWei);
        uint256 gasSell = g - gasleft();
        uint256 usdcBack = USDC.balanceOf(address(t)) - u1;
        emit log_named_uint("sell: MON sent (wei)", valueWei);
        emit log_named_uint("sell: USDC recv (6dp)", usdcBack);
        emit log_named_uint("sell: gas", gasSell);
        emit log_named_uint("MON left (wei)", address(t).balance);
        assertGt(usdcBack, 0);
    }

    // below minSize (2e12 units = 200 MON): does market path enforce it?
    function test_smallBuySell() public {
        t.buy(1e8); // 1 USDC
        uint256 mon = address(t).balance;
        emit log_named_uint("small buy MON recv (wei)", mon);
        uint256 u1 = USDC.balanceOf(address(t));
        t.sell(uint96(mon / 1e8), (mon / 1e8) * 1e8);
        emit log_named_uint("small sell USDC recv", USDC.balanceOf(address(t)) - u1);
    }
}
