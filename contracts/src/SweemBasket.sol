// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {Math} from "@openzeppelin/contracts/utils/math/Math.sol";
import {SafeCast} from "@openzeppelin/contracts/utils/math/SafeCast.sol";
import {IAccountCore} from "./interfaces/IAccountCore.sol";
import {IKuruOrderBook} from "./interfaces/IKuruOrderBook.sol";
import {KuruMath} from "./libraries/KuruMath.sol";
import {SweemBasketFactory} from "./SweemBasketFactory.sol";

struct Component {
    address market;
    uint16 weightBps;
}

contract SweemBasket is ERC20, ReentrancyGuard {
    using SafeERC20 for IERC20;
    using KuruMath for IKuruOrderBook;

    error Expired();
    error Slippage();
    error ZeroAmount();
    error Paused();
    error MarketNotAllowed();
    error BadWeights();
    error DuplicateMarket();

    event Minted(address indexed user, uint256 usdcIn, uint256 shares);
    event Redeemed(address indexed user, uint256 shares, uint256 usdcOut);

    SweemBasketFactory public immutable factory;
    IAccountCore public immutable accountCore;
    IERC20 public immutable usdc;

    Component[] internal _components;
    address[] public tokens; // base token per component, native MON = address(0)

    constructor(string memory name_, string memory symbol_, Component[] memory comps) ERC20(name_, symbol_) {
        factory = SweemBasketFactory(msg.sender);
        accountCore = IAccountCore(factory.accountCore());
        usdc = IERC20(factory.usdc());
        uint256 sum;
        for (uint256 i; i < comps.length; ++i) {
            address m = comps[i].market;
            if (!factory.isMarketAllowed(m) || IKuruOrderBook(m).quoteToken() != address(usdc)) {
                revert MarketNotAllowed();
            }
            if (comps[i].weightBps == 0) revert BadWeights();
            for (uint256 j; j < i; ++j) {
                if (comps[j].market == m) revert DuplicateMarket();
            }
            sum += comps[i].weightBps;
            _components.push(comps[i]);
            tokens.push(IKuruOrderBook(m).baseToken());
        }
        if (sum != 10_000) revert BadWeights();
    }

    function mint(uint256 usdcIn, uint256 minShares, uint256 deadline) external nonReentrant returns (uint256 shares) {
        if (block.timestamp > deadline) revert Expired();
        if (usdcIn == 0) revert ZeroAmount();
        if (factory.paused()) revert Paused();

        uint256 n = _components.length;
        uint256 supply = totalSupply();
        uint256[] memory held = holdings();
        uint256[] memory alloc = new uint256[](n);
        if (supply == 0) {
            for (uint256 i; i < n; ++i) alloc[i] = usdcIn * _components[i].weightBps / 10_000;
        } else {
            uint256 total;
            for (uint256 i; i < n; ++i) {
                alloc[i] = IKuruOrderBook(_components[i].market).valueAtMid(held[i]);
                total += alloc[i];
            }
            for (uint256 i; i < n; ++i) alloc[i] = Math.mulDiv(usdcIn, alloc[i], total);
        }

        uint256 cash0 = accountCore.getBalance(address(this), address(usdc));
        usdc.safeTransferFrom(msg.sender, address(this), usdcIn);
        usdc.forceApprove(address(accountCore), usdcIn);
        accountCore.deposit(address(usdc), usdcIn);

        shares = type(uint256).max;
        for (uint256 i; i < n; ++i) {
            address m = _components[i].market;
            if (!factory.isMarketAllowed(m)) revert MarketNotAllowed();
            if (alloc[i] == 0) continue;
            IKuruOrderBook(m).swap(0, true, SafeCast.toUint128(alloc[i]), 0, SafeCast.toUint64(deadline));
            if (supply != 0 && held[i] != 0) {
                uint256 bought = accountCore.getBalance(address(this), tokens[i]) - held[i];
                shares = Math.min(shares, Math.mulDiv(supply, bought, held[i]));
            }
        }

        // unfilled USDC (thin book, lot rounding) goes back to the minter
        uint256 refund = accountCore.getBalance(address(this), address(usdc)) - cash0;
        uint256 spent = usdcIn - refund;
        if (supply == 0) shares = spent * 1e12;
        else if (shares == type(uint256).max) shares = 0;
        if (shares == 0) revert ZeroAmount();
        if (shares < minShares) revert Slippage();

        if (refund != 0) {
            accountCore.withdraw(address(usdc), refund);
            usdc.safeTransfer(msg.sender, refund);
        }
        _mint(msg.sender, shares);
        emit Minted(msg.sender, spent, shares);
    }

    function redeem(uint256 shares, uint256 minUsdcOut, uint256 deadline)
        external
        nonReentrant
        returns (uint256 usdcOut)
    {
        if (block.timestamp > deadline) revert Expired();
        if (shares == 0) revert ZeroAmount();

        uint256 supply = totalSupply();
        uint256[] memory held = holdings();
        _burn(msg.sender, shares);

        uint256 cash0 = accountCore.getBalance(address(this), address(usdc));
        for (uint256 i; i < held.length; ++i) {
            IKuruOrderBook m = IKuruOrderBook(_components[i].market);
            uint256 amt = m.floorToLot(Math.mulDiv(held[i], shares, supply));
            if (amt != 0) m.swap(0, false, SafeCast.toUint128(amt), 0, SafeCast.toUint64(deadline));
        }
        usdcOut = accountCore.getBalance(address(this), address(usdc)) - cash0;
        if (usdcOut < minUsdcOut) revert Slippage();

        if (usdcOut != 0) {
            accountCore.withdraw(address(usdc), usdcOut);
            usdc.safeTransfer(msg.sender, usdcOut);
        }
        emit Redeemed(msg.sender, shares, usdcOut);
    }

    function components() external view returns (Component[] memory) {
        return _components;
    }

    /// Base token balances held in this basket's AccountCore account, raw atoms.
    function holdings() public view returns (uint256[] memory h) {
        h = new uint256[](tokens.length);
        for (uint256 i; i < h.length; ++i) h[i] = accountCore.getBalance(address(this), tokens[i]);
    }

    /// 0 until the first mint deposits into AccountCore (a zero deposit reverts).
    function accountId() external view returns (uint40) {
        return accountCore.userRegistry(address(this));
    }
}
