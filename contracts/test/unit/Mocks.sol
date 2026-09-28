// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {IKuruOrderBook} from "../../src/interfaces/IKuruOrderBook.sol";

contract MockERC20 is ERC20 {
    uint8 internal immutable _dec;

    constructor(string memory s, uint8 d) ERC20(s, s) {
        _dec = d;
    }

    function decimals() public view override returns (uint8) {
        return _dec;
    }

    function mint(address to, uint256 a) external {
        _mint(to, a);
    }
}

/// Ledger only; books move balances via `move`. Pre-fund it with USDC so withdrawals settle.
contract MockAccountCore {
    mapping(address => mapping(address => uint256)) public getBalance;
    mapping(address => uint40) public userRegistry;
    uint40 internal nextId = 1;

    function deposit(address token, uint256 amount) external payable {
        require(amount != 0, "zero");
        if (token == address(0)) require(msg.value == amount, "value");
        else IERC20(token).transferFrom(msg.sender, address(this), amount);
        if (userRegistry[msg.sender] == 0) userRegistry[msg.sender] = nextId++;
        getBalance[msg.sender][token] += amount;
    }

    function withdraw(address token, uint256 amount) external {
        getBalance[msg.sender][token] -= amount;
        if (token == address(0)) payable(msg.sender).transfer(amount);
        else IERC20(token).transfer(msg.sender, amount);
    }

    function move(address user, address debitToken, uint256 debit, address creditToken, uint256 credit) external {
        getBalance[user][debitToken] -= debit;
        getBalance[user][creditToken] += credit;
    }
}

/// Infinite-depth book: buys fill at ask, sells at bid, taker fee off the output, `fillBps` models thin books.
contract MockOrderBook {
    MockAccountCore public immutable ac;
    address public immutable baseToken;
    address public immutable quoteToken;
    uint32 public immutable pricePrecision;
    uint96 public immutable sizePrecision;
    uint256 public immutable baseSizeMultiplier;
    uint32 public bid;
    uint32 public ask;
    uint256 public fillBps = 10_000;
    uint256 public constant FEE_PPS = 7000;

    constructor(MockAccountCore ac_, address base, address quote, uint32 pp, uint96 sp, uint256 mult) {
        (ac, baseToken, quoteToken, pricePrecision, sizePrecision, baseSizeMultiplier) = (ac_, base, quote, pp, sp, mult);
    }

    function setPx(uint32 b, uint32 a) external {
        (bid, ask) = (b, a);
    }

    function setFill(uint256 bps) external {
        fillBps = bps;
    }

    function bestBidAsk() external view returns (uint32, uint32) {
        return (bid, ask);
    }

    function getMarketParams() external view returns (uint32, uint96, uint32, uint96, uint96, uint256, uint256) {
        return (pricePrecision, sizePrecision, 1, 1e7, 5e12, FEE_PPS, 4000);
    }

    function swap(uint40, bool isBuy, uint128 amountIn, uint128 minOut, uint64 deadline)
        external
        returns (IKuruOrderBook.SwapResult memory r)
    {
        require(block.timestamp <= deadline, "deadline");
        uint256 denom = uint256(pricePrecision) * sizePrecision * baseSizeMultiplier;
        uint256 used;
        uint256 out;
        if (isBuy) {
            if (ask == 0) return r;
            uint256 base = uint256(amountIn) * fillBps / 10_000 * denom / (uint256(ask) * 1e6);
            base -= base % baseSizeMultiplier;
            used = base * ask * 1e6 / denom;
            out = base - base * FEE_PPS / 1e7;
            ac.move(msg.sender, quoteToken, used, baseToken, out);
        } else {
            if (bid == 0 || bid == type(uint32).max) return r;
            used = uint256(amountIn) * fillBps / 10_000;
            used -= used % baseSizeMultiplier;
            uint256 q = used * bid * 1e6 / denom;
            out = q - q * FEE_PPS / 1e7;
            ac.move(msg.sender, baseToken, used, quoteToken, out);
        }
        require(out >= minOut, "minOut");
        r = IKuruOrderBook.SwapResult(uint128(used), uint128(out));
    }
}
