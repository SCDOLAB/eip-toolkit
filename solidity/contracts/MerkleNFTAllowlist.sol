// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/token/ERC721/ERC721.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/utils/cryptography/MerkleProof.sol";

contract MerkleNFTAllowlist is ERC721, Ownable, ReentrancyGuard {
    uint256 public totalSupply;
    uint256 public constant MAX_SUPPLY = 1000;
    uint256 public constant WL_PRICE = 0.001 ether;
    uint256 public constant PUBLIC_PRICE = 0.002 ether;
    uint256 public constant MAX_PER_WL = 2;
    uint256 public constant MAX_PER_PUBLIC = 5;

    bool public wlMintActive;
    bool public publicMintActive;
    bytes32 public merkleRoot;

    mapping(address => uint256) public wlMintedCount;
    mapping(address => uint256) public publicMintedCount;

    event Minted(address indexed to, uint256 quantity, bool fromAllowlist);

    constructor(address initialOwner) ERC721("MerkleNFT", "MNFT") Ownable(initialOwner) {}

    function setMerkleRoot(bytes32 _root) external onlyOwner {
        merkleRoot = _root;
    }

    function toggleWlMint() external onlyOwner {
        wlMintActive = !wlMintActive;
    }

    function togglePublicMint() external onlyOwner {
        publicMintActive = !publicMintActive;
    }

    function wlMint(uint256 quantity, bytes32[] calldata proof) external payable nonReentrant {
        require(wlMintActive, "WL mint closed");
        require(quantity > 0, "quantity zero");
        require(totalSupply + quantity <= MAX_SUPPLY, "max supply reached");
        require(msg.value == WL_PRICE * quantity, "incorrect eth amount");
        require(wlMintedCount[msg.sender] + quantity <= MAX_PER_WL, "exceed wl limit");

        bytes32 leaf = keccak256(abi.encodePacked(msg.sender, MAX_PER_WL));
        require(MerkleProof.verify(proof, merkleRoot, leaf), "not in allowlist");

        wlMintedCount[msg.sender] += quantity;
        totalSupply += quantity;

        for (uint256 i = 0; i < quantity; i++) {
            _safeMint(msg.sender, totalSupply - quantity + i + 1);
        }
        emit Minted(msg.sender, quantity, true);
    }

    function publicMint(uint256 quantity) external payable nonReentrant {
        require(publicMintActive, "Public mint closed");
        require(quantity > 0, "quantity zero");
        require(totalSupply + quantity <= MAX_SUPPLY, "max supply reached");
        require(msg.value == PUBLIC_PRICE * quantity, "incorrect eth for public");
        require(publicMintedCount[msg.sender] + quantity <= MAX_PER_PUBLIC, "exceed public mint limit");

        publicMintedCount[msg.sender] += quantity;
        totalSupply += quantity;

        for (uint256 i = 0; i < quantity; i++) {
            _safeMint(msg.sender, totalSupply - quantity + i + 1);
        }
        emit Minted(msg.sender, quantity, false);
    }

    function withdraw() external onlyOwner {
        payable(owner()).transfer(address(this).balance);
    }
}
