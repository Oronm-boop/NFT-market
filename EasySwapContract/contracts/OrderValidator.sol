// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/**
 * @title OrderValidator
 * @notice 订单校验与成交状态：校验订单参数合法性，并维护订单的已成交量与取消状态。
 * @dev 业务职责：
 *   - 校验订单必填项（maker、过期时间、salt、NFT 资产、价格等），支持按需跳过过期校验（如取消时）。
 *   - 通过 filledAmount 记录每笔订单的已成交数量；值为 CANCELLED 表示已取消，不可再撮合。
 *   - 提供更新已成交量、取消订单的接口，供订单簿在成交或用户取消时调用。
 */

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {ContextUpgradeable} from "@openzeppelin/contracts-upgradeable/utils/ContextUpgradeable.sol";
import {EIP712Upgradeable} from "@openzeppelin/contracts-upgradeable/utils/cryptography/EIP712Upgradeable.sol";

import {Price} from "./libraries/RedBlackTreeLibrary.sol";
import {LibOrder, OrderKey} from "./libraries/LibOrder.sol";

abstract contract OrderValidator is
    Initializable,
    ContextUpgradeable,
    EIP712Upgradeable
{
    bytes4 private constant EIP_1271_MAGIC_VALUE = 0x1626ba7e;

    /// @dev 订单取消标记：filledAmount[key] == CANCELLED 表示该订单已取消
    uint256 private constant CANCELLED = type(uint256).max;

    /// @dev 订单 key -> 已成交量；CANCELLED 表示已取消，不可再匹配
    mapping(OrderKey => uint256) public filledAmount;

    function __OrderValidator_init(
        string memory EIP712Name,
        string memory EIP712Version
    ) internal onlyInitializing {
        __Context_init();
        __EIP712_init(EIP712Name, EIP712Version);
        __OrderValidator_init_unchained();
    }

    function __OrderValidator_init_unchained() internal onlyInitializing {}

    /**
     * @notice 校验订单参数是否合法（maker、过期时间、salt、NFT/价格等）
     * @param order 待校验订单
     * @param isSkipExpiry 为 true 时跳过过期时间校验（如取消订单时）
     */
    function _validateOrder(
        LibOrder.Order memory order,
        bool isSkipExpiry
    ) internal view {
        require(order.maker != address(0), "OVa: miss maker");

        if (!isSkipExpiry) {
            require(
                order.expiry == 0 || order.expiry > block.timestamp,
                "OVa: expired"
            );
        }
        require(order.salt != 0, "OVa: zero salt");

        if (order.side == LibOrder.Side.List) {
            require(
                order.nft.collection != address(0),
                "OVa: unsupported nft asset"
            );
        } else if (order.side == LibOrder.Side.Bid) {
            require(Price.unwrap(order.price) > 0, "OVa: zero price");
        }
    }

    /**
     * @notice 查询订单已成交量；若订单已取消则 revert
     * @param orderKey 订单哈希
     * @return orderFilledAmount 已成交数量（未成交为 0）
     */
    function _getFilledAmount(
        OrderKey orderKey
    ) internal view returns (uint256 orderFilledAmount) {
        orderFilledAmount = filledAmount[orderKey];
        require(orderFilledAmount != CANCELLED, "OVa: canceled");
    }

    /**
     * @notice 更新订单已成交量（成交后由订单簿调用）
     * @param newAmount 新的已成交数量
     * @param orderKey 订单哈希
     */
    function _updateFilledAmount(
        uint256 newAmount,
        OrderKey orderKey
    ) internal {
        require(newAmount != CANCELLED, "OVa: canceled");
        filledAmount[orderKey] = newAmount;
    }

    /**
     * @notice 取消订单（将已成交量设为 CANCELLED，后续撮合会 revert）
     * @param orderKey 订单哈希
     */
    function _cancelOrder(OrderKey orderKey) internal {
        filledAmount[orderKey] = CANCELLED;
    }

    uint256[50] private __gap;
}
