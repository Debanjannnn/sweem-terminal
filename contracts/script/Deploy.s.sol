// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Script, console2} from "forge-std/Script.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";

import {SUSD} from "../src/tokens/SUSD.sol";
import {SweemVault} from "../src/SweemVault.sol";
import {IndexOracle} from "../src/IndexOracle.sol";
import {MarketRegistry} from "../src/MarketRegistry.sol";
import {IndexPerpEngine} from "../src/IndexPerpEngine.sol";

/// @notice Deploys the full sweem stack. Target: Monad testnet, chain 10143.
/// forge script script/Deploy.s.sol --rpc-url $MONAD_RPC_URL --broadcast
contract Deploy is Script {
    uint256 constant WAD = 1e18;
    uint256 constant YEAR = 365 days;

    // Underlying is ETH/USD. Monad testnet publishes exactly five Chainlink feeds
    // (ETH, BTC, LINK, USDC, USDT / USD) and no MON/USD; Kuru's MON/USDC book is
    // empty, so it cannot back an index today. A vol index needs a series that moves.
    bytes32 constant VOL_MARKET = keccak256("VOL-ETH-USD");
    bytes32 constant POW2_MARKET = keccak256("POW2-ETH-USD");
    bytes32 constant POWHALF_MARKET = keccak256("POWHALF-ETH-USD");

    // Canonical oracle-service feed ids. feedId = keccak256(bytes(these)) and the
    // strings must match services/oracle/src/config.ts exactly — the publisher sends
    // the hash, and the contract rejects anything else.
    string constant VOL_FEED = "sweem:vol:ETH/USD";
    string constant POW2_FEED = "sweem:pow:ETH/USD:k=2000000000000000000";
    string constant POWHALF_FEED = "sweem:pow:ETH/USD:k=500000000000000000";

    function run() external {
        uint256 pk = vm.envUint("PRIVATE_KEY");
        address admin = vm.addr(pk);
        // Both writers hold the SAME role. The contract cannot tell them apart, which
        // is exactly what makes CRE optional rather than load-bearing.
        address writer = vm.envOr("ORACLE_WRITER", admin);
        address forwarder = vm.envOr("CHAINLINK_FORWARDER", address(0));

        vm.startBroadcast(pk);

        SUSD susd = new SUSD();

        SweemVault vault = new SweemVault(
            IERC20(address(susd)),
            admin,
            5_000_000e6, // TVL cap: 5M USDC
            0.8e18, // max utilization 80%
            (uint256(0.02e18)) / YEAR, // base 2% APR, per second
            (uint256(0.2e18)) / YEAR // slope 20% APR at 100% utilization
        );

        MarketRegistry registry = new MarketRegistry(admin);
        IndexPerpEngine engine = new IndexPerpEngine(vault, registry, admin);
        vault.grantRole(vault.ENGINE_ROLE(), address(engine));

        // sigma: annualized realized vol of ETH/USD, 1e18 (0.8e18 == 80%)
        IndexOracle volOracle = new IndexOracle(VOL_FEED, 1 hours, 3_000, admin);
        // normalized (P/P0)^2 and (P/P0)^0.5 on ETH/USD
        IndexOracle pow2Oracle = new IndexOracle(POW2_FEED, 1 hours, 2_000, admin);
        IndexOracle powHalfOracle = new IndexOracle(POWHALF_FEED, 1 hours, 2_000, admin);

        _grantWriter(volOracle, writer, forwarder);
        _grantWriter(pow2Oracle, writer, forwarder);
        _grantWriter(powHalfOracle, writer, forwarder);

        // vol perp: index IS sigma. k = 0, so no power funding term.
        registry.addMarket(
            VOL_MARKET,
            MarketRegistry.MarketParams({
                indexOracle: volOracle,
                volOracle: IndexOracle(address(0)),
                k: 0,
                maxOiPerSide: 500_000e18,
                maxLeverage: 3e18,
                minPositionSize: 10e18,
                maintenanceMarginBps: 800,
                openFeeBps: 10,
                closeFeeBps: 10,
                liquidationFeeBps: 500,
                fundingK: 1e18,
                fundingCapPerSec: uint256(1e18) / YEAR * 100, // |100% APR| cap
                skewScale: 1_000_000e18,
                enabled: true
            })
        );

        // power perps: funding couples to the vol index via k(k-1)/2 * sigma^2
        registry.addMarket(POW2_MARKET, _powerParams(pow2Oracle, volOracle, 2e18));
        registry.addMarket(POWHALF_MARKET, _powerParams(powHalfOracle, volOracle, 0.5e18));

        // seed the oracles so markets are immediately tradeable
        volOracle.pushIndex(volOracle.feedId(), 0.8e18);
        pow2Oracle.pushIndex(pow2Oracle.feedId(), 1e18);
        powHalfOracle.pushIndex(powHalfOracle.feedId(), 1e18);

        // sUSD for the admin, and an LP deposit so the vault can actually back a
        // position — an empty vault rejects every open, so a zero seed means a
        // deployment that looks fine and trades nothing.
        uint256 mintAmount = vm.envOr("SUSD_MINT", uint256(1_000_000e6));
        uint256 seedDeposit = vm.envOr("VAULT_SEED", uint256(250_000e6));
        susd.mint(admin, mintAmount);
        if (seedDeposit > 0) {
            susd.approve(address(vault), seedDeposit);
            vault.deposit(seedDeposit, admin);
        }

        vm.stopBroadcast();

        _write(
            address(susd),
            address(vault),
            address(registry),
            address(engine),
            address(volOracle),
            address(pow2Oracle),
            address(powHalfOracle)
        );
    }

    function _grantWriter(IndexOracle o, address writer, address forwarder) internal {
        o.grantRole(o.WRITER_ROLE(), writer);
        if (forwarder != address(0)) o.grantRole(o.WRITER_ROLE(), forwarder);
    }

    function _powerParams(IndexOracle idx, IndexOracle vol, int256 k)
        internal
        pure
        returns (MarketRegistry.MarketParams memory)
    {
        return MarketRegistry.MarketParams({
            indexOracle: idx,
            volOracle: vol,
            k: k,
            maxOiPerSide: 500_000e18,
            maxLeverage: 4e18,
            minPositionSize: 10e18,
            maintenanceMarginBps: 625,
            openFeeBps: 10,
            closeFeeBps: 10,
            liquidationFeeBps: 500,
            fundingK: 1e18,
            fundingCapPerSec: uint256(1e18) / YEAR * 100,
            skewScale: 1_000_000e18,
            enabled: true
        });
    }

    function _write(
        address susd,
        address vault,
        address registry,
        address engine,
        address volOracle,
        address pow2Oracle,
        address powHalfOracle
    ) internal {
        string memory o = "sweem";
        vm.serializeUint(o, "chainId", block.chainid);
        vm.serializeAddress(o, "SUSD", susd);
        vm.serializeAddress(o, "SweemVault", vault);
        vm.serializeAddress(o, "MarketRegistry", registry);
        vm.serializeAddress(o, "IndexPerpEngine", engine);
        vm.serializeAddress(o, "VolIndexOracle", volOracle);
        vm.serializeAddress(o, "Power2IndexOracle", pow2Oracle);
        vm.serializeAddress(o, "PowerHalfIndexOracle", powHalfOracle);
        // registry market key (engine) and oracle feed string (service) are different
        // namespaces; publish both so neither side has to re-derive the other.
        vm.serializeString(o, "VolFeedMarketId", VOL_FEED);
        vm.serializeString(o, "Power2FeedMarketId", POW2_FEED);
        vm.serializeString(o, "PowerHalfFeedMarketId", POWHALF_FEED);
        vm.serializeBytes32(o, "VolMarketKey", VOL_MARKET);
        vm.serializeBytes32(o, "Power2MarketKey", POW2_MARKET);
        string memory json = vm.serializeBytes32(o, "PowerHalfMarketKey", POWHALF_MARKET);
        string memory path = string.concat("./deployments/", vm.toString(block.chainid), ".json");
        vm.writeJson(json, path);
        console2.log("addresses written to", path);
    }
}
