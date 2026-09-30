// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import {IExchange} from "./IExchange.sol";

interface IERC20g {
    function approve(address, uint256) external returns (bool);
    function transfer(address, uint256) external returns (bool);
}

interface IStableg {
    function get_dy(int128, int128, uint256) external view returns (uint256);
    function exchange(int128, int128, uint256, uint256) external returns (uint256);
}

// Runs the scheduler's guard step sequences (sched.Guard + planner.ResizeBasis) and checks position margin really rises.
contract PerplGuardTest is Test {
    IExchange constant EX = IExchange(0x34B6552d57a35a1D042CcAe1951BD1C370112a6F);
    IERC20g constant AUSD = IERC20g(0x00000000eFE302BEAA2b3e6e1b18d08D69a9012a);
    IERC20g constant USDC = IERC20g(0x754704Bc059F8C67012fEd69BC8A327a5aafb603);
    IStableg constant POOL = IStableg(0x942644106B073E30D72c2C5D7529D5C296ea91ab);
    uint256 constant BTC = 1; // price dec 1, lot dec 5 ⇒ notional CNS = mark × lot
    address user = makeAddr("user");
    uint256 id;
    uint256 mark;

    function _exec(uint8 t, uint256 px, uint256 lot, uint256 lev, uint256 amt) internal {
        EX.execOrder(IExchange.OrderDesc({orderDescId: block.number, perpId: BTC, orderType: IExchange.OrderDescEnum.wrap(t),
            orderId: 0, pricePNS: px, lotLNS: lot, expiryBlock: 0, postOnly: false, fillOrKill: false, immediateOrCancel: t == 1 || t == 3,
            maxMatches: 0, leverageHdths: lev, lastExecutionBlock: 0, amountCNS: amt, maxNegPnlCollatBPS: t == 5 ? 0 : 1000}));
    }

    // margin fraction in bps of notional, plus free balance
    function _state() internal view returns (uint256 fracBps, uint256 lot, uint256 dep, int256 equity, uint256 free) {
        (IExchange.PositionInfo memory pos, uint256 m,) = EX.getPosition(BTC, id);
        equity = int256(pos.depositCNS) + pos.pnlCNS;
        fracBps = uint256(equity) * 10_000 / (m * pos.lotLNS);
        return (fracBps, pos.lotLNS, pos.depositCNS, equity, EX.getAccountById(id).balanceCNS);
    }

    function setUp() public {
        vm.createSelectFork("monad");
        vm.prank(address(POOL));
        USDC.transfer(user, 200e6);
        vm.startPrank(user, user);
        USDC.approve(address(POOL), type(uint256).max);
        uint256 got = POOL.exchange(1, 0, 60e6, POOL.get_dy(1, 0, 60e6) * 999 / 1000);
        AUSD.approve(address(EX), type(uint256).max);
        id = EX.createAccount(got);
        mark = EX.getPerpetualInfo(BTC).markPNS;
        _exec(1, mark * 98 / 100, 400, 1000, 0); // 0.004 BTC short at 10x ⇒ ~10% margin
        vm.stopPrank();
    }

    // top-up path: swap → depositCollateral → addMargin(deposited)
    function test_topUpRaisesMargin() public {
        (uint256 before,, uint256 dep0,,) = _state();
        vm.startPrank(user, user);
        uint256 ausd = POOL.exchange(1, 0, 20e6, 19_890_000); // planner minOut at 50 bps slip
        EX.depositCollateral(ausd);
        (uint256 mid,, uint256 depMid,,) = _state();
        assertEq(depMid, dep0, "depositCollateral must not touch the position (isolated)");
        assertEq(mid, before);
        _exec(5, 0, 0, 100, 19_890_000); // addMargin = deposit step amount (minOut)
        vm.stopPrank();
        (uint256 afterBps,, uint256 dep1,,) = _state();
        console.log("top-up margin bps", before, "->", afterBps);
        assertEq(dep1, dep0 + 19_890_000);
        assertGt(afterBps, before);
    }

    // cut path: closeShort cut → addMargin(free + released estimate); fraction = equity / kept notional
    function test_cutRaisesMargin() public {
        (uint256 before, uint256 lot,, int256 equity, uint256 free) = _state();
        uint256 cut = 240; // keep 160 of 400
        uint256 notional = mark * lot;
        uint256 released = (uint256(equity) - notional / 1000) * cut / lot; // sched.Guard: (equity − 0.1% notional) × cut / lot
        vm.startPrank(user, user);
        _exec(3, mark * 102 / 100, cut, 100, 0);
        (uint256 mid,,,, uint256 freeMid) = _state();
        assertApproxEqRel(mid, before, 0.01e18, "a cut alone keeps the fraction (collateral released pro rata)");
        assertGe(freeMid, free + released, "released estimate must be covered by free balance");
        _exec(5, 0, 0, 100, free + released);
        vm.stopPrank();
        (uint256 afterBps, uint256 lot1,, int256 eq1,) = _state();
        console.log("cut margin bps", before, "->", afterBps);
        console.log("free before", free);
        assertEq(lot1, lot - cut);
        uint256 expect = (uint256(equity) + free) * 10_000 / (mark * lot1); // sched.Guard keep math: equity + free over kept notional
        assertApproxEqRel(afterBps, expect, 0.02e18);
        assertGt(afterBps, before * 2);
        eq1;
    }
}
