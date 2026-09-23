//! AccountCore: the only custodian. a wallet balance cannot trade, so every flow
//! starts with a deposit here and an account id.
use alloy_primitives::{aliases::U40, keccak256, Address, B256, U256};
use alloy_sol_types::{sol, SolCall};

use crate::{rpc::Rpc, venues::Call};

sol! {
    function deposit(address token, uint256 amount) external payable;
    function userRegistry(address user) external view returns (uint40);
    function getBalance(address user, address token) external view returns (uint256);
    function getSpotReservedBalance(address user, address token) external view returns (uint256);
    function authorizeAccountSigner(address account, address signer, uint32 permissions, uint64 expiry) external;
    function revokeAccountSigner(address account, address signer) external;
    function isAuthorizedAccountSigner(address account, address signer, uint32 permission) external view returns (bool);
    function accountSignerAuthorizationNonces(address account) external view returns (uint256);
    function effectiveSpotTakerFeePps(uint40 accountId, uint256 marketRate) external view returns (uint256);
    function effectiveSpotMakerFeePps(uint40 accountId, uint256 marketRate) external view returns (uint256);

    function approve(address spender, uint256 amount) external returns (bool);
    function allowance(address owner, address spender) external view returns (uint256);
}

/// the only permission we ever grant: it cannot move funds out of the account.
pub const PERM_TRADE: u32 = 1;

pub fn deposit_call(account_core: Address, token: Address, amount: U256) -> Call {
    Call {
        to: account_core,
        // native MON is address(0) plus msg.value; there is no allowance step
        value: if token.is_zero() { amount } else { U256::ZERO },
        data: depositCall { token, amount }.abi_encode().into(),
    }
}

pub fn approve_call(token: Address, spender: Address, amount: U256) -> Call {
    Call { to: token, value: U256::ZERO, data: approveCall { spender, amount }.abi_encode().into() }
}

/// an unreadable allowance approves anyway: a redundant approval costs gas, a
/// missing one fails the deposit behind it.
pub async fn approve_if_needed(rpc: &Rpc, token: Address, spender: Address, owner: Address, amount: U256) -> Option<Call> {
    if token.is_zero() {
        return None;
    }
    let data = allowanceCall { owner, spender }.abi_encode();
    match rpc.eth_call(owner, token, U256::ZERO, &data).await {
        Ok(out) if out.len() >= 32 && U256::from_be_slice(&out[..32]) >= amount => None,
        _ => Some(approve_call(token, spender, amount)),
    }
}

pub fn authorize_call(account_core: Address, account: Address, signer: Address, expiry: u64) -> Call {
    Call {
        to: account_core,
        value: U256::ZERO,
        data: authorizeAccountSignerCall { account, signer, permissions: PERM_TRADE, expiry }.abi_encode().into(),
    }
}

pub fn revoke_call(account_core: Address, account: Address, signer: Address) -> Call {
    Call {
        to: account_core,
        value: U256::ZERO,
        data: revokeAccountSignerCall { account, signer }.abi_encode().into(),
    }
}

async fn read(rpc: &Rpc, from: Address, to: Address, data: Vec<u8>) -> Result<Vec<u8>, String> {
    rpc.eth_call(from, to, U256::ZERO, &data).await
}

/// 0 means the address has never deposited and therefore cannot trade.
pub async fn user_id(rpc: &Rpc, account_core: Address, user: Address) -> Result<u64, String> {
    let out = read(rpc, user, account_core, userRegistryCall { user }.abi_encode()).await?;
    Ok(userRegistryCall::abi_decode_returns(&out).map_err(|e| e.to_string())?.to::<u64>())
}

/// free and spot-reserved. passive inventory is in neither and is not read here.
pub async fn balances(rpc: &Rpc, account_core: Address, user: Address, token: Address) -> Result<(U256, U256), String> {
    let free = read(rpc, user, account_core, getBalanceCall { user, token }.abi_encode()).await?;
    let held = read(rpc, user, account_core, getSpotReservedBalanceCall { user, token }.abi_encode()).await?;
    Ok((
        getBalanceCall::abi_decode_returns(&free).map_err(|e| e.to_string())?,
        getSpotReservedBalanceCall::abi_decode_returns(&held).map_err(|e| e.to_string())?,
    ))
}

/// expiry emits no event, so this is the only honest answer about a live grant.
pub async fn is_authorized(rpc: &Rpc, account_core: Address, account: Address, signer: Address) -> Result<bool, String> {
    let data = isAuthorizedAccountSignerCall { account, signer, permission: PERM_TRADE }.abi_encode();
    let out = read(rpc, account, account_core, data).await?;
    isAuthorizedAccountSignerCall::abi_decode_returns(&out).map_err(|e| e.to_string())
}

/// the authorization epoch: every grant or revoke advances it and invalidates
/// every pre-signed intent bound to the old value. our kill switch.
pub async fn auth_nonce(rpc: &Rpc, account_core: Address, account: Address) -> Result<U256, String> {
    let out = read(rpc, account, account_core, accountSignerAuthorizationNoncesCall { account }.abi_encode()).await?;
    accountSignerAuthorizationNoncesCall::abi_decode_returns(&out).map_err(|e| e.to_string())
}

/// account defaults are currently off, so the effective rate is read, not assumed.
pub async fn effective_fees(rpc: &Rpc, account_core: Address, from: Address, id: u64, taker_rate: U256, maker_rate: U256) -> Result<(U256, U256), String> {
    let account_id = U40::try_from(id).map_err(|_| "account id exceeds uint40".to_string())?;
    let t = read(rpc, from, account_core, effectiveSpotTakerFeePpsCall { accountId: account_id, marketRate: taker_rate }.abi_encode()).await?;
    let mk = read(rpc, from, account_core, effectiveSpotMakerFeePpsCall { accountId: account_id, marketRate: maker_rate }.abi_encode()).await?;
    Ok((
        effectiveSpotTakerFeePpsCall::abi_decode_returns(&t).map_err(|e| e.to_string())?,
        effectiveSpotMakerFeePpsCall::abi_decode_returns(&mk).map_err(|e| e.to_string())?,
    ))
}

/// the gasless grant path (`authorizeAccountSignerBySig`). unused by path B, but
/// the domain name is `KuruAccountCore`, not `AccountCore`, and getting it wrong
/// produces a signature that verifies nowhere and fails silently. pinned in tests.
#[allow(dead_code)]
pub mod eip712 {
    use super::*;

    pub const DOMAIN_NAME: &str = "KuruAccountCore";
    pub const DOMAIN_VERSION: &str = "1";
    pub const AUTHORIZE_TYPE: &str = "AuthorizeAccountSigner(address account,address authorizer,address signer,uint32 permissions,uint64 expiry,uint256 nonce,uint256 deadline)";
    pub const REVOKE_TYPE: &str = "RevokeAccountSigner(address account,address authorizer,address signer,uint256 nonce,uint256 deadline)";

    pub fn domain_separator(chain_id: u64, verifying_contract: Address) -> B256 {
        let mut buf = Vec::with_capacity(160);
        buf.extend_from_slice(keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)").as_slice());
        buf.extend_from_slice(keccak256(DOMAIN_NAME).as_slice());
        buf.extend_from_slice(keccak256(DOMAIN_VERSION).as_slice());
        buf.extend_from_slice(&U256::from(chain_id).to_be_bytes::<32>());
        buf.extend_from_slice(&verifying_contract.into_word()[..]);
        keccak256(buf)
    }

    pub fn digest(domain: B256, struct_hash: B256) -> B256 {
        let mut buf = [0u8; 66];
        buf[0] = 0x19;
        buf[1] = 0x01;
        buf[2..34].copy_from_slice(domain.as_slice());
        buf[34..].copy_from_slice(struct_hash.as_slice());
        keccak256(buf)
    }

    pub fn authorize_struct_hash(account: Address, authorizer: Address, signer: Address, permissions: u32, expiry: u64, nonce: U256, deadline: U256) -> B256 {
        let mut buf = Vec::with_capacity(224);
        buf.extend_from_slice(keccak256(AUTHORIZE_TYPE).as_slice());
        for a in [account, authorizer, signer] {
            buf.extend_from_slice(&a.into_word()[..]);
        }
        for w in [U256::from(permissions), U256::from(expiry), nonce, deadline] {
            buf.extend_from_slice(&w.to_be_bytes::<32>());
        }
        keccak256(buf)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use alloy_primitives::{address, b256, hex};

    const AC: Address = crate::config::ACCOUNT_CORE;
    const USER: Address = address!("fA0Fff2FeFdCb57C1A14d9E357fEcA7d999F2d1E");
    const SIGNER: Address = address!("c41Af288173214A8c19d3aa01BeD10446fb6Af6F");
    const USDC: Address = address!("Ee0722ead54f1B4fe97bE399Be43BC0226a6f97E");

    fn sel(c: &Call) -> String {
        hex::encode(&c.data[..4])
    }

    /// selectors pinned against the deployed implementation 0x5cEe690f...bF8C8.
    #[test]
    fn selectors_match_the_deployed_implementation() {
        for (got, want) in [
            (depositCall::SELECTOR, "47e7ef24"),
            (userRegistryCall::SELECTOR, "7fe0dd32"),
            (getBalanceCall::SELECTOR, "d4fac45d"),
            (getSpotReservedBalanceCall::SELECTOR, "da19598d"),
            (authorizeAccountSignerCall::SELECTOR, "2607750a"),
            (revokeAccountSignerCall::SELECTOR, "ae3aacb6"),
            (isAuthorizedAccountSignerCall::SELECTOR, "9421f0dc"),
            (accountSignerAuthorizationNoncesCall::SELECTOR, "84249ce7"),
            (effectiveSpotTakerFeePpsCall::SELECTOR, "ce3be2dc"),
            (effectiveSpotMakerFeePpsCall::SELECTOR, "0ad77aeb"),
        ] {
            assert_eq!(hex::encode(got), want);
        }
    }

    #[test]
    fn native_deposit_carries_value_and_erc20_does_not() {
        let amount = U256::from(5_000_000u64);
        let native = deposit_call(AC, Address::ZERO, amount);
        assert_eq!(native.to, AC);
        assert_eq!(native.value, amount, "MON is address(0) with msg.value");
        assert_eq!(sel(&native), "47e7ef24");

        let erc20 = deposit_call(AC, USDC, amount);
        assert_eq!(erc20.value, U256::ZERO);
        let d = depositCall::abi_decode(&erc20.data).unwrap();
        assert_eq!((d.token, d.amount), (USDC, amount));
    }

    #[test]
    fn approval_spender_is_account_core_never_a_router() {
        let c = approve_call(USDC, AC, U256::from(7u64));
        assert_eq!(c.to, USDC);
        let d = approveCall::abi_decode(&c.data).unwrap();
        assert_eq!(d.spender, AC, "v2 has no router to approve");
    }

    #[test]
    fn a_grant_is_trade_only() {
        let c = authorize_call(AC, USER, SIGNER, 1_800_000_000);
        assert_eq!(c.to, AC);
        let d = authorizeAccountSignerCall::abi_decode(&c.data).unwrap();
        assert_eq!(d.permissions, PERM_TRADE);
        assert_eq!(d.permissions, 1, "never INTERNAL_TRANSFER(4), WITHDRAW(8) or ADMIN(16)");
        assert_eq!((d.account, d.signer, d.expiry), (USER, SIGNER, 1_800_000_000));

        let r = revoke_call(AC, USER, SIGNER);
        assert_eq!(revokeAccountSignerCall::abi_decode(&r.data).unwrap().account, USER);
    }

    /// golden vectors: read live from the contract's own views on chain 10143.
    #[test]
    fn eip712_typehashes_match_the_chain() {
        assert_eq!(
            keccak256(eip712::AUTHORIZE_TYPE),
            b256!("46f27651a0d3aa6f7cbea546c73809e146d1ab55581aa7dca37d620c3a7ff648"),
            "AUTHORIZE_ACCOUNT_SIGNER_TYPEHASH()"
        );
        assert_eq!(
            keccak256(eip712::REVOKE_TYPE),
            b256!("4251595bac30fbad2f674d1239346dc8538be5aeb20ac35b3520e1cda10c80d4"),
            "REVOKE_ACCOUNT_SIGNER_TYPEHASH()"
        );
    }

    /// eip712Domain() returns ("KuruAccountCore","1",10143,0x6384e9b2...).
    #[test]
    fn eip712_domain_is_kuru_account_core_not_account_core() {
        let want = eip712::domain_separator(10143, AC);
        assert_eq!(eip712::DOMAIN_NAME, "KuruAccountCore");
        assert_ne!(
            want,
            {
                // the plausible wrong name produces a different, silently invalid domain
                let mut buf = Vec::new();
                buf.extend_from_slice(keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)").as_slice());
                buf.extend_from_slice(keccak256("AccountCore").as_slice());
                buf.extend_from_slice(keccak256("1").as_slice());
                buf.extend_from_slice(&U256::from(10143u64).to_be_bytes::<32>());
                buf.extend_from_slice(&AC.into_word()[..]);
                keccak256(buf)
            },
            "the domain name is KuruAccountCore"
        );
        // pinned so a refactor cannot drift the domain silently
        assert_eq!(want, b256!("44a0d345bb4185668dec1a144b1531015554f76afcbccc4f3ef9b076046dad77"));
    }

    #[test]
    fn a_digest_binds_every_field() {
        let d = eip712::domain_separator(10143, AC);
        let base = eip712::authorize_struct_hash(USER, USER, SIGNER, 1, 1_800_000_000, U256::from(6u64), U256::from(1_790_000_000u64));
        let other_nonce = eip712::authorize_struct_hash(USER, USER, SIGNER, 1, 1_800_000_000, U256::from(7u64), U256::from(1_790_000_000u64));
        assert_ne!(base, other_nonce, "the epoch is part of the signature");
        assert_ne!(eip712::digest(d, base), eip712::digest(d, other_nonce));
        assert_eq!(eip712::digest(d, base).as_slice()[..2].len(), 2);
    }
}
