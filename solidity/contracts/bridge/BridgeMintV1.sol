// SPDX-License-Identifier: MIT
pragma solidity ^0.5.16;

/// @title BridgeMint (SCDO V1.0.0 compatible)
/// @notice Minimal pegged ERC20 for SCDO. Compiled with solc 0.5.x; the
///         resulting bytecode is patched (REVERT 0xfd -> STOP 0x00) before
///         deployment because SCDO V1.0.0's EVM does not support Byzantium.
contract BridgeMint {
    event Minted(address indexed to, uint256 amount, uint256 indexed sourceNonce);
    event Burned(address indexed from, uint256 amount, uint256 indexed withdrawNonce);
    event MinterUpdated(address oldMinter, address newMinter);

    string public name;
    string public symbol;
    uint8 public decimals = 18;
    uint256 public totalSupply;

    address public minter;
    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;
    mapping(uint256 => bool) public usedNonces;

    constructor(string memory _name, string memory _symbol, address _minter) public {
        name = _name;
        symbol = _symbol;
        minter = _minter;
        emit MinterUpdated(address(0), _minter);
    }

    function transfer(address to, uint256 amount) public returns (bool) {
        _transfer(msg.sender, to, amount);
        return true;
    }

    function approve(address spender, uint256 amount) public returns (bool) {
        allowance[msg.sender][spender] = amount;
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) public returns (bool) {
        require(allowance[from][msg.sender] >= amount);
        allowance[from][msg.sender] -= amount;
        _transfer(from, to, amount);
        return true;
    }

    function mint(address to, uint256 amount, uint256 sourceNonce) public {
        require(msg.sender == minter);
        require(!usedNonces[sourceNonce]);
        usedNonces[sourceNonce] = true;
        balanceOf[to] += amount;
        totalSupply += amount;
        emit Minted(to, amount, sourceNonce);
    }

    function burn(uint256 amount, uint256 withdrawNonce) public {
        require(balanceOf[msg.sender] >= amount);
        balanceOf[msg.sender] -= amount;
        totalSupply -= amount;
        emit Burned(msg.sender, amount, withdrawNonce);
    }

    function setMinter(address newMinter) public {
        require(msg.sender == minter);
        emit MinterUpdated(minter, newMinter);
        minter = newMinter;
    }

    function _transfer(address from, address to, uint256 amount) internal {
        require(balanceOf[from] >= amount);
        balanceOf[from] -= amount;
        balanceOf[to] += amount;
    }
}
