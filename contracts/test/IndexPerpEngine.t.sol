// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Vm} from "forge-std/Test.sol";
import {BaseTest} from "./Base.t.sol";
import {IndexPerpEngine} from "../src/IndexPerpEngine.sol";
import {MarketRegistry} from "../src/MarketRegistry.sol";

contract IndexPerpEngineTest is BaseTest {
    function test_openLongRecordsPositionAndOi() public {
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);

        IndexPerpEngine.Position memory p = engine.getPosition(VOL, alice);
        assertEq(p.size, 100_000e18);
        assertEq(p.entryIndex, 0.8e18);
        assertEq(p.entryNotional, 80_000e18);
        // 10 bps open fee on 80k notional
        assertEq(p.collateral, 50_000e18 - 80e18);

        (,, uint256 longOI, uint256 shortOI,,) = engine.marketState(VOL);
        assertEq(longOI, 80_000e18);
        assertEq(shortOI, 0);
        assertEq(engine.totalOpenNotional(), 80_000e18);
    }

    function test_longProfitsWhenIndexRises() public {
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
        uint256 before = susd.balanceOf(alice);

        _publish(volOracle, 0.9e18); // +0.1 * 100k = +10k USD
        vm.prank(alice);
        engine.closePosition(VOL);

        uint256 gained = susd.balanceOf(alice) - before;
        // 50k collateral + 10k pnl - open fee - close fee (~170)
        assertApproxEqRel(gained, 60_000e6, 0.01e18);
    }

    function test_shortProfitsWhenIndexFalls() public {
        vm.prank(alice);
        engine.openPosition(VOL, -100_000e18, 50_000e6);
        uint256 before = susd.balanceOf(alice);

        _publish(volOracle, 0.7e18);
        vm.prank(alice);
        engine.closePosition(VOL);
        assertApproxEqRel(susd.balanceOf(alice) - before, 60_000e6, 0.01e18);
    }

    function test_lossFlowsToTheVault() public {
        uint256 vaultBefore = vault.totalAssets();
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
        _publish(volOracle, 0.7e18); // -10k for the long
        vm.prank(alice);
        engine.closePosition(VOL);
        assertGt(vault.totalAssets(), vaultBefore + 9_000e6, "vault must capture the trader loss");
    }

    function test_modifyAddsAndRemovesCollateral() public {
        vm.startPrank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
        engine.modifyPosition(VOL, 0, 10_000e6);
        assertApproxEqAbs(engine.getPosition(VOL, alice).collateral, 60_000e18, 200e18);
        engine.modifyPosition(VOL, 0, -20_000e6);
        assertApproxEqAbs(engine.getPosition(VOL, alice).collateral, 40_000e18, 200e18);
        vm.stopPrank();
    }

    function test_modifyCannotBreachMaxLeverage() public {
        vm.startPrank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
        // 80k notional at 3x max leverage needs >= 26.6k collateral
        vm.expectRevert(IndexPerpEngine.LeverageTooHigh.selector);
        engine.modifyPosition(VOL, 0, -45_000e6);
        vm.stopPrank();
    }

    function test_maxLeverageEnforcedAtOpen() public {
        // 3x cap: 100k size at index 0.8 = 80k notional, needs >= 26_667 collateral
        vm.prank(alice);
        vm.expectRevert(IndexPerpEngine.LeverageTooHigh.selector);
        engine.openPosition(VOL, 100_000e18, 20_000e6);
    }

    function test_minPositionSizeEnforced() public {
        vm.prank(alice);
        vm.expectRevert(IndexPerpEngine.PositionTooSmall.selector);
        engine.openPosition(VOL, 1e18, 100e6); // 0.8 USD notional < 10 USD min
    }

    function test_oiCapEnforced() public {
        vm.startPrank(admin);
        MarketRegistry.MarketParams memory p = registry.params(VOL);
        p.maxOiPerSide = 50_000e18;
        registry.setParams(VOL, p);
        vm.stopPrank();

        vm.prank(alice);
        vm.expectRevert(IndexPerpEngine.OiCapExceeded.selector);
        engine.openPosition(VOL, 100_000e18, 50_000e6); // 80k > 50k cap
    }

    function test_disabledMarketRejectsOpens() public {
        vm.prank(admin);
        registry.setEnabled(VOL, false);
        vm.prank(alice);
        vm.expectRevert(IndexPerpEngine.MarketDisabled.selector);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
    }

    function test_guardianHaltAndGlobalPause() public {
        vm.prank(admin);
        engine.haltMarket(VOL, true);
        vm.prank(alice);
        vm.expectRevert(IndexPerpEngine.MarketIsHalted.selector);
        engine.openPosition(VOL, 100_000e18, 50_000e6);

        vm.startPrank(admin);
        engine.haltMarket(VOL, false);
        engine.pause();
        vm.stopPrank();
        vm.prank(alice);
        vm.expectRevert();
        engine.openPosition(VOL, 100_000e18, 50_000e6);
    }

    function test_borrowAccruesAndIsPaidToTheVault() public {
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
        uint256 vaultBefore = vault.totalAssets();

        skip(365 days);
        _publish(volOracle, 0.8e18); // keep the index flat and the feed fresh
        vm.prank(alice);
        engine.closePosition(VOL);

        (uint256 borrowCum,,,,,) = engine.marketState(VOL);
        assertGt(borrowCum, 0, "borrow index never advanced");
        assertGt(vault.totalAssets(), vaultBefore, "vault did not receive the borrow");
    }

    function test_skewFundingChargesTheCrowdedSide() public {
        vm.prank(alice);
        engine.openPosition(VOL, 300_000e18, 150_000e6); // long-heavy book
        skip(30 days);
        _publish(volOracle, 0.8e18);
        engine.accrue(VOL);
        (, int256 fundingCum,,,,) = engine.marketState(VOL);
        assertGt(fundingCum, 0, "long-heavy skew must make longs pay");
    }

    function test_powerMarketFundingIncludesTheVolCoupling() public {
        // Balanced book on both markets isolates the k(k-1)/2 sigma^2 term.
        vm.prank(alice);
        engine.openPosition(POW2, 100_000e18, 50_000e6);
        vm.prank(bob);
        engine.openPosition(POW2, -100_000e18, 50_000e6);
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
        vm.prank(bob);
        engine.openPosition(VOL, -100_000e18, 50_000e6);

        skip(30 days);
        _publish(volOracle, 0.8e18);
        _publish(pow2Oracle, 1e18);
        engine.accrue(POW2);
        engine.accrue(VOL);

        (, int256 powFunding,,,,) = engine.marketState(POW2);
        (, int256 volFunding,,,,) = engine.marketState(VOL);
        assertEq(volFunding, 0, "balanced non-power market has no funding");
        assertGt(powFunding, 0, "power perp longs must pay k(k-1)/2 sigma^2");
        // k=2, sigma=0.8 => 0.64 per year; 30 days is 30/365 of that
        assertApproxEqRel(powFunding, int256(0.64e18 * 30 days / YEAR), 0.01e18);
    }

    function test_powerFundingScalesWithTheVolIndex() public {
        vm.prank(alice);
        engine.openPosition(POW2, 100_000e18, 50_000e6);
        vm.prank(bob);
        engine.openPosition(POW2, -100_000e18, 50_000e6);

        skip(10 days);
        _publish(volOracle, 0.8e18);
        _publish(pow2Oracle, 1e18);
        engine.accrue(POW2);
        (, int256 lowVol,,,,) = engine.marketState(POW2);

        skip(10 days);
        _publish(volOracle, 1.6e18); // sigma doubles => variance term quadruples
        _publish(pow2Oracle, 1e18);
        engine.accrue(POW2);
        (, int256 total,,,,) = engine.marketState(POW2);

        assertApproxEqRel(total - lowVol, lowVol * 4, 0.01e18);
    }

    function test_healthyPositionIsNeverLiquidatable() public {
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6); // 62.5% margin, mm is 8%
        vm.prank(keeper);
        vm.expectRevert(IndexPerpEngine.StillHealthy.selector);
        engine.liquidate(VOL, alice);
    }

    function test_undercollateralizedPositionIsAlwaysLiquidatable() public {
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 30_000e6); // 80k notional, 30k collateral

        _publish(volOracle, 0.55e18); // -0.25 * 100k = -25k, leaves ~5k on 55k notional (9%)
        _publish(volOracle, 0.5e18); // -30k total, collateral ~0

        uint256 keeperBefore = susd.balanceOf(keeper);
        vm.prank(keeper);
        engine.liquidate(VOL, alice);

        assertEq(engine.getPosition(VOL, alice).size, 0, "position must be closed");
        assertGe(susd.balanceOf(keeper), keeperBefore, "keeper is paid the liquidation share");
        (,, uint256 longOI,,,) = engine.marketState(VOL);
        assertEq(longOI, 0, "open interest must be released");
    }

    function test_liquidationSplitsThePenaltyWithTheVault() public {
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 30_000e6);
        _publish(volOracle, 0.56e18); // 24k loss, ~6k left on 56k notional == 10.7%
        _publish(volOracle, 0.54e18); // 26k loss, ~4k left on 54k notional == 7.4% < 8% mm

        uint256 keeperBefore = susd.balanceOf(keeper);
        uint256 vaultBefore = vault.totalAssets();
        vm.prank(keeper);
        engine.liquidate(VOL, alice);

        uint256 reward = susd.balanceOf(keeper) - keeperBefore;
        assertGt(reward, 0);
        // penalty is 5% of 54k notional == 2.7k, half to the keeper
        assertApproxEqRel(reward, 1_350e6, 0.05e18);
        assertGt(vault.totalAssets(), vaultBefore);
    }

    function test_badDebtIsAbsorbedByLps() public {
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 30_000e6);
        _publish(volOracle, 0.6e18);
        _publish(volOracle, 0.4e18); // -40k on 30k collateral => 10k bad debt

        vm.prank(keeper);
        engine.liquidate(VOL, alice);
        assertGt(vault.badDebt(), 0, "bad debt must be recorded against LPs");
        assertEq(engine.totalCollateral(), 0);
    }

    function testFuzz_liquidationBoundaryIsExact(uint256 priceBps) public {
        priceBps = bound(priceBps, 4_000, 12_000);
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 30_000e6);

        uint256 newIndex = 0.8e18 * priceBps / 10_000;
        _publish(volOracle, newIndex);
        engine.accrue(VOL);

        IndexPerpEngine.Position memory p = engine.getPosition(VOL, alice);
        int256 pnl = p.size * (int256(newIndex) - int256(p.entryIndex)) / int256(WAD);
        int256 equity = int256(p.collateral) + pnl;
        uint256 notional = 100_000e18 * newIndex / WAD;
        bool unhealthy = equity <= 0 || uint256(equity) * WAD / notional < 800 * 1e14;

        vm.prank(keeper);
        if (unhealthy) {
            engine.liquidate(VOL, alice);
            assertEq(engine.getPosition(VOL, alice).size, 0);
        } else {
            vm.expectRevert(IndexPerpEngine.StillHealthy.selector);
            engine.liquidate(VOL, alice);
        }
    }

    function test_engineHoldsExactlyTheSumOfCollateral() public {
        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
        vm.prank(bob);
        engine.openPosition(VOL, -60_000e18, 40_000e6);

        uint256 required = (engine.totalCollateral() + 1e12 - 1) / 1e12;
        assertEq(susd.balanceOf(address(engine)), required);
    }

    /// @notice Accrual conservation, exactly: with funding switched off and the index
    ///         held flat, every wei of borrow charged to the trader appears in the vault,
    ///         alongside the open and close fees. Nothing is created or lost.
    function test_borrowChargedEqualsBorrowReceived() public {
        vm.startPrank(admin);
        MarketRegistry.MarketParams memory p = registry.params(VOL);
        p.fundingK = 0;
        registry.setParams(VOL, p);
        vm.stopPrank();

        vm.prank(alice);
        engine.openPosition(VOL, 100_000e18, 50_000e6);
        // the open fee has already landed in the vault by the time we sample it
        uint256 vaultAfterOpen = vault.totalAssets();

        skip(180 days);
        _publish(volOracle, 0.8e18);

        vm.recordLogs();
        vm.prank(alice);
        engine.closePosition(VOL);

        Vm.Log[] memory logs = vm.getRecordedLogs();
        uint256 borrowPaid;
        bytes32 sig = keccak256("PositionSettled(bytes32,address,int256,uint256,int256,uint256,uint256)");
        for (uint256 i; i < logs.length; i++) {
            if (logs[i].topics[0] == sig) {
                (, borrowPaid,,,) = abi.decode(logs[i].data, (int256, uint256, int256, uint256, uint256));
            }
        }
        assertGt(borrowPaid, 0, "no borrow accrued over 180 days");

        uint256 closeFee = 80_000e18 * 10 / 10_000;
        uint256 vaultGain = vault.toWad(vault.totalAssets() - vaultAfterOpen);
        assertApproxEqAbs(vaultGain, borrowPaid + closeFee, 1e12, "vault gain != borrow + close fee");
        assertEq(engine.totalCollateral(), 0);
    }
}
