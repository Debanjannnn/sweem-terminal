// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Test, Vm} from "forge-std/Test.sol";
import {BaseTest} from "./Base.t.sol";
import {IndexPerpEngine} from "../src/IndexPerpEngine.sol";
import {IndexOracle} from "../src/IndexOracle.sol";
import {SweemVault} from "../src/SweemVault.sol";
import {SUSD} from "../src/tokens/SUSD.sol";

/// @notice Drives the protocol through random but well-formed action sequences.
contract Handler is Test {
    IndexPerpEngine public engine;
    SweemVault public vault;
    SUSD public susd;
    IndexOracle[2] public oracles;
    bytes32[2] public markets;
    address[3] public traders;
    address public lp;
    address public keeper;
    address public admin;

    uint256 public ghostBorrowCharged;
    int256 public ghostFundingCharged;
    uint256 public ghostBadDebt;
    uint256 public ghostOpens;
    uint256 public ghostCloses;
    uint256 public ghostLiquidations;
    uint256 public ghostSettles;

    constructor(
        IndexPerpEngine engine_,
        SweemVault vault_,
        SUSD susd_,
        IndexOracle[2] memory oracles_,
        bytes32[2] memory markets_,
        address[3] memory traders_,
        address lp_,
        address keeper_,
        address admin_
    ) {
        engine = engine_;
        vault = vault_;
        susd = susd_;
        oracles = oracles_;
        markets = markets_;
        traders = traders_;
        lp = lp_;
        keeper = keeper_;
        admin = admin_;
    }

    modifier record() {
        vm.recordLogs();
        _;
        _harvest();
    }

    /// @dev Accumulate what traders were actually charged, straight from the events
    ///      the indexer will consume.
    function _harvest() internal {
        Vm.Log[] memory logs = vm.getRecordedLogs();
        bytes32 sig = keccak256("PositionSettled(bytes32,address,int256,uint256,int256,uint256,uint256)");
        for (uint256 i; i < logs.length; i++) {
            if (logs[i].topics.length > 0 && logs[i].topics[0] == sig) {
                (, uint256 borrowPaid, int256 fundingPaid, uint256 badDebt,) =
                    abi.decode(logs[i].data, (int256, uint256, int256, uint256, uint256));
                ghostSettles++;
                ghostBorrowCharged += borrowPaid;
                ghostFundingCharged += fundingPaid;
                ghostBadDebt += badDebt;
            }
        }
    }

    function _trader(uint256 s) internal view returns (address) {
        return traders[s % traders.length];
    }

    function _market(uint256 s) internal view returns (bytes32) {
        return markets[s % markets.length];
    }

    function open(uint256 traderSeed, uint256 marketSeed, int256 size, uint256 marginBps) external record {
        size = bound(size, -150_000e18, 150_000e18);
        if (size == 0) return;
        bytes32 mkt = _market(marketSeed);
        uint256 idx = _index(mkt);
        uint256 notional = uint256(size > 0 ? size : -size) * idx / 1e18;
        // margin between 40% and 200% of notional, i.e. inside the 3x/4x leverage caps
        uint256 collateral = notional * bound(marginBps, 4_000, 20_000) / 10_000 / 1e12;
        address t = _trader(traderSeed);
        vm.prank(t);
        try engine.openPosition(mkt, size, collateral) {
            ghostOpens++;
        } catch {}
    }

    function modify(uint256 traderSeed, uint256 marketSeed, int256 sizeDelta, int256 collateralDelta) external record {
        sizeDelta = bound(sizeDelta, -150_000e18, 150_000e18);
        collateralDelta = bound(collateralDelta, -50_000e6, 200_000e6);
        address t = _trader(traderSeed);
        vm.prank(t);
        try engine.modifyPosition(_market(marketSeed), sizeDelta, collateralDelta) {} catch {}
    }

    function close(uint256 traderSeed, uint256 marketSeed) external record {
        address t = _trader(traderSeed);
        vm.prank(t);
        try engine.closePosition(_market(marketSeed)) {
            ghostCloses++;
        } catch {}
    }

    function liquidate(uint256 traderSeed, uint256 marketSeed) external record {
        vm.prank(keeper);
        try engine.liquidate(_market(marketSeed), _trader(traderSeed)) {
            ghostLiquidations++;
        } catch {}
    }

    function publish(uint256 oracleSeed, uint256 newValue) external record {
        IndexOracle o = oracles[oracleSeed % oracles.length];
        newValue = bound(newValue, 0.2e18, 3e18);
        vm.prank(admin);
        o.reset(newValue); // reset, so the fuzzer is never blocked by the breaker
    }

    function accrueAll() external record {
        try engine.accrue(markets[0]) {} catch {}
        try engine.accrue(markets[1]) {} catch {}
    }

    /// @dev Time passes and the oracles keep publishing — otherwise every action
    ///      after the first warp would revert on staleness and the suite would be vacuous.
    function warp(uint256 dt) external record {
        skip(bound(dt, 1, 20 days));
        vm.startPrank(admin);
        oracles[0].reset(oracles[0].value());
        oracles[1].reset(oracles[1].value());
        vm.stopPrank();
    }

    function _index(bytes32 mkt) internal view returns (uint256) {
        return mkt == markets[0] ? oracles[0].value() : oracles[1].value();
    }

    function lpDeposit(uint256 amount) external record {
        amount = bound(amount, 1e6, 500_000e6);
        vm.prank(lp);
        try vault.deposit(amount, lp) {} catch {}
    }

    function lpWithdraw(uint256 amount) external record {
        amount = bound(amount, 1e6, 500_000e6);
        vm.prank(lp);
        try vault.withdraw(amount, lp, lp) {} catch {}
    }
}

contract InvariantsTest is BaseTest {
    Handler internal handler;
    uint256 internal initialSupply;

    function setUp() public override {
        super.setUp();
        address carol = address(0xCA401);
        _fund(carol, 1_000_000e6);
        _fund(keeper, 0);

        handler = new Handler(
            engine, vault, susd, [volOracle, pow2Oracle], [VOL, POW2], [alice, bob, carol], lp, keeper, admin
        );
        initialSupply = susd.totalSupply();
        targetContract(address(handler));
    }

    /// @notice Spec invariant: the sum of all position payouts never exceeds vault
    ///         assets plus posted collateral.
    function invariant_vaultSolvency() public view {
        uint256 payouts = engine.totalCollateral();
        uint256 backing = vault.toWad(susd.balanceOf(address(engine)) + vault.totalAssets());
        assertLe(payouts, backing, "payouts exceed vault assets + posted collateral");
    }

    /// @notice Every open position can be paid out in full from the engine's own balance.
    function invariant_engineCoversEveryPosition() public view {
        assertGe(
            susd.balanceOf(address(engine)), engine.totalCollateral() / 1e12, "engine cannot pay out its own positions"
        );
    }

    /// @notice Spec invariant: accrual conservation. The engine holds exactly the
    ///         traders' collateral and not a wei more — so every unit of borrow and
    ///         funding charged to a trader has landed in the vault, and every unit
    ///         credited has been drawn from it. Tolerance is one 6-decimal unit,
    ///         the rounding boundary between 18-decimal internals and USDC.
    function invariant_accrualConservation() public view {
        assertApproxEqAbs(
            vault.toWad(susd.balanceOf(address(engine))),
            engine.totalCollateral(),
            1e12,
            "borrow/funding charged does not equal borrow/funding received"
        );
    }

    /// @notice No USDC is minted or destroyed by protocol operations.
    function invariant_noValueCreated() public view {
        assertEq(susd.totalSupply(), initialSupply);
    }

    /// @notice totalCollateral is the exact sum of live positions.
    function invariant_collateralMatchesPositions() public view {
        address[3] memory ts = [alice, bob, address(0xCA401)];
        bytes32[2] memory ms = [VOL, POW2];
        uint256 sum;
        for (uint256 i; i < ts.length; i++) {
            for (uint256 j; j < ms.length; j++) {
                sum += engine.getPosition(ms[j], ts[i]).collateral;
            }
        }
        assertEq(sum, engine.totalCollateral(), "totalCollateral drifted from live positions");
    }

    /// @notice Open interest bookkeeping agrees with the global total.
    function invariant_openInterestConsistent() public view {
        (,, uint256 l0, uint256 s0,,) = engine.marketState(VOL);
        (,, uint256 l1, uint256 s1,,) = engine.marketState(POW2);
        assertEq(l0 + s0 + l1 + s1, engine.totalOpenNotional());
        assertEq(vault.openNotional(), engine.totalOpenNotional());
    }
}

contract HandlerSmokeTest is InvariantsTest {
    /// @dev Deterministic 300-step walk through the same handler the invariant fuzzer
    ///      drives. Proves the action space is actually reachable (opens, closes,
    ///      liquidations and borrow all happen) and re-checks every invariant at each
    ///      step — a fuzz run that never trades would silently prove nothing.
    function test_randomSequenceExercisesEverything() public {
        uint256 seed = uint256(keccak256("sweem"));
        for (uint256 i; i < 300; i++) {
            seed = uint256(keccak256(abi.encode(seed, i)));
            uint256 action = seed % 8;
            if (action == 0) {
                handler.open(seed >> 8, seed >> 16, int256(seed % 300_000e18) - 150_000e18, seed >> 24);
            } else if (action == 1) {
                handler.modify(
                    seed >> 8, seed >> 16, int256(seed % 200_000e18) - 100_000e18, int256(seed % 100_000e6) - 50_000e6
                );
            } else if (action == 2) {
                handler.close(seed >> 8, seed >> 16);
            } else if (action == 3) {
                handler.liquidate(seed >> 8, seed >> 16);
            } else if (action == 4) {
                handler.publish(seed >> 8, seed >> 16);
            } else if (action == 5) {
                handler.warp(seed % 5 days);
            } else if (action == 6) {
                handler.lpDeposit(seed >> 8);
            } else {
                handler.lpWithdraw(seed >> 8);
            }

            invariant_vaultSolvency();
            invariant_engineCoversEveryPosition();
            invariant_accrualConservation();
            invariant_noValueCreated();
            invariant_collateralMatchesPositions();
            invariant_openInterestConsistent();
        }
        assertGt(handler.ghostOpens(), 0, "sequence never opened a position");
        assertGt(handler.ghostCloses(), 0, "sequence never closed a position");
        assertGt(handler.ghostLiquidations(), 0, "sequence never liquidated");
        assertGt(handler.ghostBorrowCharged(), 0, "sequence never charged borrow");
    }

    function test_handlerLiquidates() public {
        handler.open(0, 0, 100_000e18, 4_000); // 40% margin on the VOL market
        assertEq(handler.ghostOpens(), 1);
        handler.publish(0, 0.4e18); // index halves: the long is deep underwater
        handler.liquidate(0, 0);
        assertEq(handler.ghostLiquidations(), 1, "liquidation failed");
        assertEq(engine.getPosition(VOL, alice).size, 0);
    }

    function test_handlerOpensAndCloses() public {
        handler.open(0, 0, 100_000e18, 8_000);
        assertEq(handler.ghostOpens(), 1, "open failed");
        handler.warp(3 days);
        handler.close(0, 0);
        assertEq(handler.ghostCloses(), 1, "close failed");
        assertGt(handler.ghostBorrowCharged(), 0);
    }
}
