// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import "forge-std/Test.sol";
import {SweemBasket} from "../../src/SweemBasket.sol";
import {MockERC20, MockOrderBook} from "../unit/Mocks.sol";
import {Fixture} from "../unit/SweemBasket.t.sol";

contract Handler is Test {
    SweemBasket basket;
    MockERC20 usdc;
    MockOrderBook[2] books;
    address[3] actors = [address(0xA1), address(0xA2), address(0xA3)];
    uint256 public ghostMinted;
    uint256 public ghostBurned;
    bool public roundTripLoss; // redeem-all returned less than deposit - fees - tolerance

    constructor(SweemBasket b, MockERC20 u, MockOrderBook m0, MockOrderBook m1) {
        (basket, usdc, books[0], books[1]) = (b, u, m0, m1);
        for (uint256 i; i < 3; ++i) {
            u.mint(actors[i], 1e15);
            vm.prank(actors[i]);
            u.approve(address(b), type(uint256).max);
        }
    }

    function mint(uint256 actor, uint256 amt) external {
        amt = bound(amt, 1e6, 1e11);
        vm.prank(actors[actor % 3]);
        ghostMinted += basket.mint(amt, 0, block.timestamp);
    }

    function redeem(uint256 actor, uint256 bps) external {
        address a = actors[actor % 3];
        uint256 sh = basket.balanceOf(a) * bound(bps, 1, 10_000) / 10_000;
        if (sh == 0) return;
        vm.prank(a);
        basket.redeem(sh, 0, block.timestamp);
        ghostBurned += sh;
    }

    function roundTrip(uint256 amt) external {
        amt = bound(amt, 10e6, 1e11);
        address a = address(0xB0B);
        usdc.mint(a, amt);
        vm.startPrank(a);
        usdc.approve(address(basket), amt);
        uint256 bal0 = usdc.balanceOf(a);
        uint256 sh = basket.mint(amt, 0, block.timestamp);
        uint256 spent = bal0 - usdc.balanceOf(a);
        uint256 out = basket.redeem(sh, 0, block.timestamp);
        vm.stopPrank();
        ghostMinted += sh;
        ghostBurned += sh;
        // 2 taker legs 0.14% + ~0.16% spread round trip + lot rounding; 1% ceiling
        if (out + 1e4 < spent * 99 / 100) roundTripLoss = true;
    }

    function movePrice(uint256 which, uint256 bps) external {
        MockOrderBook m = books[which % 2];
        (uint32 bid, uint32 ask) = m.bestBidAsk();
        bps = bound(bps, 9000, 11_000);
        m.setPx(uint32(uint256(bid) * bps / 10_000), uint32(uint256(ask) * bps / 10_000));
    }
}

/// forge-config: default.invariant.fail-on-revert = true
contract SweemBasketInvariantTest is Fixture {
    Handler h;

    function setUp() public override {
        super.setUp();
        h = new Handler(basket, usdc, monBook, wethBook);
        targetContract(address(h));
    }

    function invariant_redeemAllCoversDepositMinusFees() public view {
        assertFalse(h.roundTripLoss());
    }

    function invariant_supplyMatchesGhostShares() public view {
        assertEq(basket.totalSupply(), h.ghostMinted() - h.ghostBurned());
    }

    function invariant_supplyBackedByHoldings() public view {
        uint256[] memory hs = basket.holdings();
        if (basket.totalSupply() != 0) assertGt(hs[0] + hs[1], 0);
        else {
            assertLt(hs[0], 1e10); // at most one MON lot of dust
            assertLt(hs[1], 1e8);
        }
        assertEq(ac.getBalance(address(basket), address(usdc)), 0); // no idle cash left behind
    }
}
