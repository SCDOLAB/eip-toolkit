// SPDX-License-Identifier: MIT
pragma solidity ^0.8.26;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";

/// @title BridgeMint
/// @notice SCDO-side pegged ERC20. Only the relayer (minter) can mint when
///         a deposit is seen on Ethereum; users burn to trigger a withdrawal.
contract BridgeMint is ERC20, Ownable {
    event Minted(address indexed to, uint256 amount, uint256 indexed sourceNonce);
    event Burned(address indexed from, uint256 amount, uint256 indexed withdrawNonce);
    event MinterUpdated(address oldMinter, address newMinter);

    address public minter;
    mapping(uint256 => bool) public usedNonces;

    constructor(
        string memory name,
        string memory symbol,
        address initialOwner,
        address initialMinter
    ) ERC20(name, symbol) Ownable(initialOwner) {
        minter = initialMinter;
        emit MinterUpdated(address(0), initialMinter);
    }

    modifier onlyMinter() {
        require(msg.sender == minter, "not minter");
        _;
    }

    /// @notice Called by relayer after seeing a Deposited event on Ethereum.
    function mint(address to, uint256 amount, uint256 sourceNonce) external onlyMinter {
        require(!usedNonces[sourceNonce], "nonce already used");
        usedNonces[sourceNonce] = true;
        _mint(to, amount);
        emit Minted(to, amount, sourceNonce);
    }

    /// @notice User burns pegged tokens; relayer sees this and releases on Ethereum.
    function burn(uint256 amount, uint256 withdrawNonce) external {
        _burn(msg.sender, amount);
        emit Burned(msg.sender, amount, withdrawNonce);
    }

    function setMinter(address newMinter) external onlyOwner {
        emit MinterUpdated(minter, newMinter);
        minter = newMinter;
    }
}
