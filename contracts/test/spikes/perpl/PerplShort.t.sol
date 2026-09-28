// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import {IExchange} from "./IExchange.sol";

interface IERC20 {
    function approve(address, uint256) external returns (bool);
    function transfer(address, uint256) external returns (bool);
    function balanceOf(address) external view returns (uint256);
}

// Minimal contract that holds a Perpl position (msg.sender = this contract).
contract Hedger {
    IExchange constant EX = IExchange(0x1964C32f0bE608E7D29302AFF5E61268E72080cc);
    IERC20 constant AUSD = IERC20(0xa9012a055bd4e0eDfF8Ce09f960291C09D5322dC);

    function open(uint256 amt) external returns (uint256) {
        AUSD.approve(address(EX), type(uint256).max);
        return EX.createAccount(amt);
    }

    function order(uint256 perpId, uint8 t, uint256 px, uint256 lot, uint256 lev, bool ioc, bool postOnly)
        external
        returns (IExchange.OrderSignature memory)
    {
        return EX.execOrder(
            IExchange.OrderDesc({
                orderDescId: block.number,
                perpId: perpId,
                orderType: IExchange.OrderDescEnum.wrap(t),
                orderId: 0,
                pricePNS: px,
                lotLNS: lot,
                expiryBlock: 0,
                postOnly: postOnly,
                fillOrKill: false,
                immediateOrCancel: ioc,
                maxMatches: 0,
                leverageHdths: lev,
                lastExecutionBlock: 0,
                amountCNS: 0,
                maxNegPnlCollatBPS: 1000
            })
        );
    }

    function withdraw(uint256 amt) external {
        EX.withdrawCollateral(amt);
    }

    function call(address to, bytes calldata data) external returns (bytes memory) {
        (bool ok, bytes memory r) = to.call(data);
        if (!ok) assembly { revert(add(r, 32), mload(r)) }
        return r;
    }
}

contract PerplShortTest is Test {
    IExchange constant EX = IExchange(0x1964C32f0bE608E7D29302AFF5E61268E72080cc);
    IERC20 constant AUSD = IERC20(0xa9012a055bd4e0eDfF8Ce09f960291C09D5322dC);
    uint256 constant BTC = 16; // price_decimals 1, size_decimals 5 (pub/context)
    uint8 constant OPEN_LONG = 0;
    uint8 constant OPEN_SHORT = 1;
    uint8 constant CLOSE_SHORT = 3;

    Hedger h;

    function setUp() public {
        vm.createSelectFork("monadt");
        h = new Hedger();
        // fund from exchange's own AUSD balance (deal() may not find Agora's storage slot)
        vm.prank(address(EX));
        AUSD.transfer(address(h), 1_000e6);
    }

    function _info() internal view returns (uint256 mark, uint256 oracle) {
        IExchange.PerpetualInfo memory p = EX.getPerpetualInfo(BTC);
        console.log("block ts", block.timestamp, "block", block.number);
        console.log("mark", p.markPNS, "markTs", p.markTimestamp);
        console.log("oracle", p.oraclePNS, "oracleTs", p.oracleTimestampSec);
        console.log("last", p.lastPNS, "lastTs", p.lastTimestamp);
        console.log("numOrders", p.numOrders, "shortOI", p.shortOpenInterestLNS);
        console.log("status", IExchange.PerpStatusEnum.unwrap(p.status), "ignOracle", p.ignOracle);
        console.log("fundingRatePct100k");
        console.logInt(p.fundingRatePct100k);
        return (p.markPNS, p.oraclePNS);
    }

    function _pos(address who) internal view returns (IExchange.PositionInfo memory pi) {
        IExchange.AccountInfo memory a = EX.getAccountByAddr(who);
        (pi,,) = EX.getPosition(BTC, a.accountId);
        console.log("acct", a.accountId, "balanceCNS", a.balanceCNS);
        console.log("pos type", IExchange.PositionEnum.unwrap(pi.positionType), "lot", pi.lotLNS);
        console.log("pos price", pi.pricePNS, "deposit", pi.depositCNS);
    }

    function test_shortOpenClose() public {
        (uint256 mark,) = _info();
        h.open(200e6); // 200 AUSD, min open = 1e8
        _pos(address(h));

        // OPEN SHORT 0.001 BTC (lot 100), IOC, limit 2% below mark, 5x
        h.order(BTC, OPEN_SHORT, mark * 98 / 100, 100, 500, true, false);
        IExchange.PositionInfo memory p = _pos(address(h));
        assertEq(p.lotLNS, 100, "short not filled");

        // CLOSE SHORT, IOC, limit 2% above mark
        h.order(BTC, CLOSE_SHORT, mark * 102 / 100, 100, 500, true, false);
        p = _pos(address(h));
        assertEq(p.lotLNS, 0, "not closed");

        IExchange.AccountInfo memory a = EX.getAccountByAddr(address(h));
        uint256 before = AUSD.balanceOf(address(h));
        h.withdraw(a.balanceCNS);
        console.log("withdrew", AUSD.balanceOf(address(h)) - before);
        assertEq(AUSD.balanceOf(address(h)) - before, a.balanceCNS);
    }

    // self-seeded: maker contract rests an ask, taker contract opens short vs... (maker long bid)
    function test_selfSeeded() public {
        (uint256 mark,) = _info();
        Hedger maker = new Hedger();
        vm.prank(address(EX));
        AUSD.transfer(address(maker), 1_000e6);
        maker.open(200e6);
        h.open(200e6);
        // maker posts bid slightly above best bid (post-only), taker sells into it
        uint256 px = mark * 999 / 1000;
        maker.order(BTC, OPEN_LONG, px, 100, 500, false, true);
        h.order(BTC, OPEN_SHORT, px, 100, 500, true, false);
        IExchange.PositionInfo memory p = _pos(address(h));
        assertEq(p.lotLNS, 100);
        console.log("fill price", p.pricePNS);
    }
}
