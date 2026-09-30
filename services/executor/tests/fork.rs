//! Monad MAINNET fork (anvil): every adapter's real calldata, signed by the local-key Signer, against the live venues.
//! run: cargo test --test fork -- --ignored --nocapture   (needs anvil + https://rpc.monad.xyz)
use alloy::{
    primitives::{Address, B256, U256},
    signers::local::PrivateKeySigner,
    sol,
    sol_types::SolCall,
};
use executor::{config::Deployments, contracts::IERC20, rpc::Rpc, signer::Signer, steps::{self, Step}};
use serde_json::{json, Value};
use std::process::{Child, Command, Stdio};

sol! {
    function bestBidAsk() external view returns (uint256, uint256);
    function s_orderIdCounter() external view returns (uint40);
    function unlockInterval() external view returns (uint256);
    function getPerpetualInfo(uint256 perpId) external view;
    function getPosition(uint256 perpId, uint256 accountId) external view;
    function getAccountByAddr(address a) external view;
}

struct Anvil(Child);
impl Drop for Anvil {
    fn drop(&mut self) {
        let _ = self.0.kill();
    }
}

struct Fork {
    rpc: Rpc,
    d: Deployments,
    signer: Signer,
    w: Address,
}

impl Fork {
    async fn step(&self, j: Value) -> String {
        let s: Step = serde_json::from_value(j.clone()).unwrap();
        let c = steps::build(&self.d, self.w, &s).unwrap();
        let h = self.signer.perform(&self.rpc, self.w, &c).await.unwrap_or_else(|e| panic!("{j}: {e}"));
        println!("ok {} {h}", j["action"]);
        h
    }
    async fn view(&self, to: Address, data: Vec<u8>) -> Vec<u8> {
        self.rpc.eth_call(to, &data).await.unwrap()
    }
    async fn word(&self, to: Address, data: Vec<u8>, i: usize) -> U256 {
        U256::from_be_slice(&self.view(to, data).await[i * 32..i * 32 + 32])
    }
    async fn bal(&self, token: &str, who: Address) -> U256 {
        self.word(self.d.tokens[token].address, IERC20::balanceOfCall { who }.abi_encode(), 0).await
    }
    async fn bal_of(&self, t: Address) -> U256 {
        self.word(t, IERC20::balanceOfCall { who: self.w }.abi_encode(), 0).await
    }
}

async fn fork() -> (Anvil, Fork) {
    let port = 18545 + (std::process::id() % 1000) as u16;
    let child = Command::new("anvil")
        .args(["--fork-url", "https://rpc.monad.xyz", "--port", &port.to_string(), "--silent"])
        .stdout(Stdio::null())
        .spawn()
        .expect("anvil on PATH");
    let rpc = Rpc::with_timeout(format!("http://127.0.0.1:{port}"), std::time::Duration::from_secs(180));
    for _ in 0..120 {
        if rpc.block_number().await.is_ok() {
            break;
        }
        tokio::time::sleep(std::time::Duration::from_millis(500)).await;
    }
    let mut d = Deployments::load(concat!(env!("CARGO_MANIFEST_DIR"), "/../../contracts/deployments/143.json")).unwrap();
    d.treasury = "0x7777777777777777777777777777777777777777".into();
    let key = PrivateKeySigner::from_bytes(&B256::repeat_byte(0x42)).unwrap();
    let w = key.address();

    // 100k MON gas + value; 3000 USDC from the 3pool (impersonated), standing in for the user's own USDC
    rpc.call("anvil_setBalance", json!([w, "0x152d02c7e14af6800000"])).await.unwrap();
    let pool = d.swap.ausd3pool.pool;
    rpc.call("anvil_impersonateAccount", json!([pool])).await.unwrap();
    rpc.call("anvil_setBalance", json!([pool, "0xde0b6b3a7640000"])).await.unwrap();
    let data = IERC20::transferCall { to: w, amount: U256::from(3_000_000_000u64) }.abi_encode();
    let h = rpc.call("eth_sendTransaction", json!([{"from": pool, "to": d.tokens["USDC"].address, "data": format!("0x{}", alloy::primitives::hex::encode(data))}])).await.unwrap();
    assert!(rpc.wait_receipt(h.as_str().unwrap(), std::time::Duration::from_secs(10)).await.unwrap().ok());
    (Anvil(child), Fork { rpc, d, signer: Signer::Local(key), w })
}

#[tokio::test]
#[ignore]
async fn mainnet_fork_every_adapter() {
    let (_anvil, f) = fork().await;
    let (w, u) = (f.w, |n: u64| U256::from(n));
    assert_eq!(f.bal("USDC", w).await, u(3_000_000_000));

    // Perpl first: it rejects marks older than 60s and anvil blocks follow wall-clock. basis leg: USDC → AUSD (3pool) → account → short 0.001 BTC → close → withdraw
    let ex = f.d.perpl.exchange;
    f.step(json!({"action":"stable.swap","from":"USDC","to":"AUSD","amount":"100000000","minOut":"99000000"})).await;
    assert!(f.bal("AUSD", w).await >= u(99_000_000));
    f.step(json!({"action":"perpl.createAccount","amount":"90000000"})).await;
    f.step(json!({"action":"perpl.deposit","amount":"5000000"})).await;
    // PerpetualInfo is dynamic: word 0 = offset, markPNS is head field 11
    let mark = f.word(ex, getPerpetualInfoCall { perpId: u(1) }.abi_encode(), 12).await;
    let acct = f.word(ex, getAccountByAddrCall { a: w }.abi_encode(), 0).await;
    println!("BTC mark {mark} account {acct}");
    f.step(json!({"action":"perpl.order","perp":"BTC","kind":"openShort","price":(mark * u(98) / u(100)).to_string(),"lot":"100","leverageHdths":200})).await;
    assert_eq!(f.word(ex, getPositionCall { perpId: u(1), accountId: acct }.abi_encode(), 6).await, u(100), "short filled");
    f.step(json!({"action":"perpl.order","perp":"BTC","kind":"closeShort","price":(mark * u(102) / u(100)).to_string(),"lot":"100","leverageHdths":200})).await;
    assert_eq!(f.word(ex, getPositionCall { perpId: u(1), accountId: acct }.abi_encode(), 6).await, U256::ZERO, "closed");
    let ausd0 = f.bal("AUSD", w).await;
    f.step(json!({"action":"perpl.withdraw","amount":"80000000"})).await;
    assert_eq!(f.bal("AUSD", w).await - ausd0, u(80_000_000));

    // fee on open → treasury
    let treasury = f.d.treasury().unwrap();
    f.step(json!({"action":"fee.transfer","amount":"400000"})).await;
    assert_eq!(f.bal("USDC", treasury).await, u(400_000));

    // Kuru wallet market buy / sell WETH
    f.step(json!({"action":"kuru.marketBuy","market":"WETH","quote":"100000000","minOut":"1"})).await;
    let weth = f.bal("WETH", w).await;
    assert!(weth > U256::ZERO);
    let usdc0 = f.bal("USDC", w).await;
    f.step(json!({"action":"kuru.marketSell","market":"WETH","size":weth.to_string(),"minOut":"1"})).await;
    assert!(f.bal("USDC", w).await > usdc0);
    assert!(f.bal("WETH", w).await < u(100_000_000), "only sub-sizePrecision dust left");

    // Kuru router MON → USDC → WETH (native first hop)
    let weth0 = f.bal("WETH", w).await;
    f.step(json!({"action":"kuru.swap","hops":[{"market":"MON","isBuy":false},{"market":"WETH","isBuy":true}],"from":"MON","to":"WETH","amount":"4000000000000000000000","minOut":"1"})).await;
    assert!(f.bal("WETH", w).await > weth0);

    // Kuru margin: deposit → limit buy 10% under bid → cancel → withdraw
    let ob = f.d.kuru.markets["WETH"].orderbook;
    f.step(json!({"action":"kuru.marginDeposit","token":"USDC","amount":"100000000"})).await;
    let bid = f.word(ob, bestBidAskCall {}.abi_encode(), 0).await;
    let px = (bid * u(9) / u(10) * u(10_000) / U256::from(10).pow(u(18))) / u(100) * u(100);
    let id = f.word(ob, s_orderIdCounterCall {}.abi_encode(), 0).await + u(1);
    f.step(json!({"action":"kuru.limit","market":"WETH","side":"buy","price":px.to_string(),"size":"100000000","postOnly":true})).await;
    f.step(json!({"action":"kuru.cancel","market":"WETH","orderIds":[id.to_string()]})).await;
    let usdc0 = f.bal("USDC", w).await;
    f.step(json!({"action":"kuru.marginWithdraw","token":"USDC","amount":"100000000"})).await;
    assert_eq!(f.bal("USDC", w).await - usdc0, u(100_000_000), "cancel refunded the margin in full");

    // Aave V3 supply / withdraw max
    let usdc0 = f.bal("USDC", w).await;
    f.step(json!({"action":"lend.supply","venue":"aaveV3","asset":"USDC","amount":"50000000"})).await;
    let a_usdc: Address = "0x35a73BAcb179d3740395A3ceCc87FF2e581d6042".parse().unwrap();
    assert!(f.bal_of(a_usdc).await >= u(49_999_999));
    f.step(json!({"action":"lend.withdraw","venue":"aaveV3","asset":"USDC","amount":"max"})).await;
    assert!(f.bal("USDC", w).await >= usdc0 - u(1));

    // ERC-4626 Euler k3USDC deposit / redeem
    let vault = f.d.lending.erc4626.iter().find(|v| v.asset == "USDC").unwrap().vault;
    let usdc0 = f.bal("USDC", w).await;
    f.step(json!({"action":"vault.deposit","vault":vault,"amount":"50000000"})).await;
    let shares = f.bal_of(vault).await;
    assert!(shares > U256::ZERO);
    f.step(json!({"action":"vault.redeem","vault":vault,"shares":shares.to_string()})).await;
    assert!(f.bal("USDC", w).await + u(2) >= usdc0);

    // shMonad deposit / instant redeem
    f.step(json!({"action":"shmonad.deposit","amount":"100000000000000000000"})).await;
    let sh = f.bal_of(f.d.staking.shmonad).await;
    assert!(sh > U256::ZERO);
    f.step(json!({"action":"shmonad.redeem","shares":sh.to_string()})).await;
    assert_eq!(f.bal_of(f.d.staking.shmonad).await, U256::ZERO);

    // Kuru ActiveVault deposit → 4-day lock → withdraw (last: the warp stales Perpl's oracle)
    let av = f.d.kuru.active_vault;
    f.step(json!({"action":"activeVault.deposit","base":"1000000000000000000000","quote":"30000000"})).await;
    let av_sh = f.bal_of(av).await;
    assert!(av_sh > U256::ZERO);
    let lock = f.word(av, unlockIntervalCall {}.abi_encode(), 0).await;
    f.rpc.call("evm_increaseTime", json!([lock.to::<u64>() + 1])).await.unwrap();
    f.rpc.call("evm_mine", json!([])).await.unwrap();
    f.step(json!({"action":"activeVault.withdraw","shares":av_sh.to_string()})).await;
    assert_eq!(f.bal_of(av).await, U256::ZERO);
}
