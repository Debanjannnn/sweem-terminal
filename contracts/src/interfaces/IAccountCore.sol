// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

interface IAccountCore {
    function deposit(address token, uint256 amount) external payable;
    function withdraw(address token, uint256 amount) external;
    function getBalance(address user, address token) external view returns (uint256);
    function userRegistry(address user) external view returns (uint40);
}
