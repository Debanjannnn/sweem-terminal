#!/bin/sh
# regenerates user_steps.golden.txt: `cast calldata` for each step of user_steps.plan.json (wallet 0x11..11, treasury 0x77..77)
W=0x1111111111111111111111111111111111111111
USDC=0x754704Bc059F8C67012fEd69BC8A327a5aafb603
WETH=0xEE8c0E9f1BFFb4Eb878d8f15f368A02a35481242
WMON=0x3bd359C1119dA7Da1D913D1C4D2B7c461115433A
OD='execOrder((uint256,uint256,uint8,uint256,uint256,uint256,uint256,bool,bool,bool,uint256,uint256,uint256,uint256,uint256))'
{
cast calldata 'transfer(address,uint256)' 0x7777777777777777777777777777777777777777 400000
cast calldata 'placeAndExecuteMarketBuy(uint96,uint256,bool,bool)' 1000000 24000000000000000 false false
cast calldata 'placeAndExecuteMarketSell(uint96,uint256,bool,bool)' 2500000000000 6000000 false false
cast calldata 'deposit(address,address,uint256)' $W $USDC 100000000
cast calldata 'withdraw(uint256,address)' 100000000 $USDC
cast calldata 'addBuyOrder(uint32,uint96,bool)' 36000000 100000000 true
cast calldata 'batchCancelOrders(uint40[])' '[1234,1235]'
cast calldata 'anyToAnySwap(address[],bool[],bool[],address,address,uint256,uint256)' '[0x065C9d28E428A0db40191a54d33d5b7c71a9C394,0xa6aFD386135B7D41A6C40C525abC4A1019b0D132]' '[false,true]' '[true,false]' 0x0000000000000000000000000000000000000000 $WETH 4000000000000000000000 1
cast calldata 'supply(address,uint256,address,uint16)' $USDC 50000000 $W 0
cast calldata 'withdraw(address,uint256,address)' $USDC 115792089237316195423570985008687907853269984665640564039457584007913129639935 $W
cast calldata 'supply(address,uint256,address,uint16)' $WMON 1000000000000000000 $W 0
cast calldata 'deposit(uint256,address)' 50000000 $W
cast calldata 'redeem(uint256,address,address)' 49000000 $W $W
cast calldata 'withdraw(uint256,address,address)' 1000000 $W $W
cast calldata 'deposit(uint256,address)' 100000000000000000000 $W
cast calldata 'redeem(uint256,address,address)' 60000000000000000000 $W $W
cast calldata 'deposit(uint256,uint256)' 1000000000000000000000 30000000
cast calldata 'withdraw(uint256)' 12345
cast calldata 'exchange(int128,int128,uint256,uint256)' 1 0 100000000 99900000
cast calldata 'createAccount(uint256)' 99900000
cast calldata 'depositCollateral(uint256)' 10000000
cast calldata "$OD" '(0,1,1,0,1100000,100,0,false,false,true,0,200,0,0,1000)'
cast calldata "$OD" '(0,1,3,0,1200000,100,0,false,false,true,0,200,0,0,1000)'
cast calldata 'withdrawCollateral(uint256)' 99000000
} > "$(dirname "$0")/user_steps.golden.txt"
