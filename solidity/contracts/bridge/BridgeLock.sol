// SPDX-License-Identifier: MIT
pragma solidity ^0.8.26;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

/// @title BridgeLock
/// @notice Ethereum-side lock contract. Users deposit ERC20 here; relayers mint
///         pegged assets on the destination chain (SCDO). Withdrawals require
///         M-of-N admin signatures.
contract BridgeLock is Ownable, ReentrancyGuard {
    using SafeERC20 for IERC20;

    event Deposited(
        address indexed token,
        address indexed user,
        uint256 amount,
        uint256 indexed nonce,
        uint256 destChainId
    );

    event Withdrawn(
        address indexed token,
        address indexed user,
        uint256 amount,
        uint256 indexed nonce
    );

    event AdminUpdated(address oldAdmin, address newAdmin);
    event FeeBpsUpdated(uint256 oldFee, uint256 newFee);

    /// @notice M-of-N admin set (we keep it simple: a single admin EOA / multisig).
    address public admin;
    /// @notice fee in basis points (e.g. 10 = 0.1%)
    uint256 public feeBps = 10;
    uint256 public constant MAX_FEE_BPS = 100; // 1%

    uint256 public depositNonce;

    modifier onlyAdmin() {
        require(msg.sender == admin, "not admin");
        _;
    }

    constructor(address initialOwner, address initialAdmin) Ownable(initialOwner) {
        admin = initialAdmin;
        emit AdminUpdated(address(0), initialAdmin);
    }

    /// @notice Lock ERC20 tokens, emit Deposited event for relayers.
    function deposit(address token, uint256 amount, uint256 destChainId) external nonReentrant {
        require(amount > 0, "amount zero");
        IERC20(token).safeTransferFrom(msg.sender, address(this), amount);

        uint256 fee = (amount * feeBps) / 10000;
        uint256 netAmount = amount - fee;

        uint256 nonce = depositNonce++;
        emit Deposited(token, msg.sender, netAmount, nonce, destChainId);
    }

    /// @notice Release locked tokens after relayer proof (signed withdrawal).
    function withdraw(
        address token,
        address to,
        uint256 amount,
        uint256 nonce,
        bytes calldata signature
    ) external nonReentrant {
        // Recover: the admin signs (to, amount, nonce, token).
        bytes32 digest = keccak256(abi.encodePacked(token, to, amount, nonce, address(this)));
        address signer = _recover(digest, signature);
        require(signer == admin, "bad signature");

        IERC20(token).safeTransfer(to, amount);
        emit Withdrawn(token, to, amount, nonce);
    }

    function setAdmin(address newAdmin) external onlyOwner {
        emit AdminUpdated(admin, newAdmin);
        admin = newAdmin;
    }

    function setFeeBps(uint256 newFee) external onlyOwner {
        require(newFee <= MAX_FEE_BPS, "fee too high");
        emit FeeBpsUpdated(feeBps, newFee);
        feeBps = newFee;
    }

    function _recover(bytes32 digest, bytes calldata sig) internal pure returns (address) {
        bytes32 ethSigned = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", digest));
        return _ecrecover(ethSigned, sig);
    }

    function _ecrecover(bytes32 hash, bytes calldata sig) internal pure returns (address) {
        require(sig.length == 65, "bad sig length");
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := calldataload(sig.offset)
            s := calldataload(add(sig.offset, 32))
            v := byte(0, calldataload(add(sig.offset, 64)))
        }
        return ecrecover(hash, v, r, s);
    }
}
