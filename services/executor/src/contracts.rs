use alloy::{primitives::U256, sol};

sol! {
    interface IERC20 {
        function approve(address spender, uint256 amount) external returns (bool);
        function transfer(address to, uint256 amount) external returns (bool);
        function allowance(address owner, address spender) external view returns (uint256);
        function balanceOf(address who) external view returns (uint256);
    }
    interface IOrderBook {
        function placeAndExecuteMarketBuy(uint96 quoteSize, uint256 minAmountOut, bool isMargin, bool isFillOrKill) external payable returns (uint256);
        function placeAndExecuteMarketSell(uint96 size, uint256 minAmountOut, bool isMargin, bool isFillOrKill) external payable returns (uint256);
        function addBuyOrder(uint32 price, uint96 size, bool postOnly) external;
        function addSellOrder(uint32 price, uint96 size, bool postOnly) external;
        function batchCancelOrders(uint40[] orderIds) external;
    }
    interface IMarginAccount {
        function deposit(address user, address token, uint256 amount) external payable;
        function withdraw(uint256 amount, address token) external;
        function getBalance(address user, address token) external view returns (uint256);
    }
    interface IRouter {
        function anyToAnySwap(address[] markets, bool[] isBuy, bool[] nativeSend, address debitToken, address creditToken, uint256 amount, uint256 minAmountOut) external payable returns (uint256);
    }
    interface IPool {
        function supply(address asset, uint256 amount, address onBehalfOf, uint16 referralCode) external;
        function withdraw(address asset, uint256 amount, address to) external returns (uint256);
    }
    // ERC-4626; shMonad has the same shape with native MON as the asset (payable deposit)
    interface IVault {
        function deposit(uint256 assets, address receiver) external payable returns (uint256);
        function withdraw(uint256 assets, address receiver, address owner) external returns (uint256);
        function redeem(uint256 shares, address receiver, address owner) external returns (uint256);
    }
    interface IActiveVault {
        function deposit(uint256 baseAmount, uint256 quoteAmount) external payable;
        function withdraw(uint256 shares) external;
    }
    interface IWMON {
        function deposit() external payable;
        function withdraw(uint256 amount) external;
    }
    interface IStableSwap {
        function exchange(int128 i, int128 j, uint256 dx, uint256 min_dy) external returns (uint256);
    }
    interface IPerpl {
        struct OrderDesc {
            uint256 orderDescId; uint256 perpId; uint8 orderType; uint256 orderId; uint256 pricePNS; uint256 lotLNS;
            uint256 expiryBlock; bool postOnly; bool fillOrKill; bool immediateOrCancel; uint256 maxMatches;
            uint256 leverageHdths; uint256 lastExecutionBlock; uint256 amountCNS; uint256 maxNegPnlCollatBPS;
        }
        function createAccount(uint256 amountCNS) external returns (uint256 accountId);
        function depositCollateral(uint256 amountCNS) external;
        function execOrder(OrderDesc orderDesc) external returns (uint256 perpId, uint256 orderId);
        function withdrawCollateral(uint256 amountCNS) external;
    }
}

pub fn u256_str(s: &str) -> Result<U256, String> {
    U256::from_str_radix(s, 10).map_err(|_| format!("not an unsigned integer: {s}"))
}
