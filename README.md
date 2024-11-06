# Pandora Chain

Pandora Chain is a decentralized platform that empowers its community to directly shape the network's evolution. Designed to provide independence, flexibility, and long-term growth for decentralized applications (dApps), Pandora ensures that decision-making and governance are in the hands of the users. Through community-driven proposals and voting, users have the ability to influence key parameters, creating a truly decentralized and participatory ecosystem.

## Key Features

- **Community Governance**: On-chain parameters can be updated through proposals made and voted on by the community, ensuring decentralized control over the platform’s evolution.
- **Sustainable Growth**: A unique economic incentive model supports continuous ecosystem development, making sure the network thrives over time.
- **EVM Compatibility**: Fully compatible with the Ethereum Virtual Machine (EVM), Pandora allows developers to easily build scalable, decentralized applications using the same tools and libraries they would use in the Ethereum ecosystem.
- **Ethereum & BSC-based**: Built on a fork of Go-Ethereum (Geth) and Binance Smart Chain (BSC), Pandora inherits the reliability and ecosystem familiarity of these established blockchain technologies.

## Consensus Mechanism: Hybrid PoA + DPoS

Pandora Chain uses a hybrid consensus mechanism that combines the best of both Proof of Authority (PoA) and Delegated Proof of Stake (DPoS). This unique hybrid approach offers a powerful balance between security, decentralization, and efficiency, creating a system that is both fast and adaptable, while remaining secure and sustainable over time.

### How It Works:

- **Proof of Authority (PoA)**: In Pandora's PoA component, trusted validators are selected based on their reputation and authority within the network. These validators are responsible for validating transactions and maintaining the integrity of the blockchain. PoA provides high throughput and lower latency, ensuring that the network can handle large volumes of transactions quickly and efficiently.
  
- **Delegated Proof of Stake (DPoS)**: On top of PoA, Pandora incorporates a community-driven election process based on DPoS. Token holders can vote for delegates (or "block producers") who are entrusted with making key decisions about the network’s development and governance. This decentralizes control over time, allowing for a more flexible, user-driven ecosystem where the community has a direct say in the future of the chain.

### Benefits of Pandora’s Hybrid Consensus:

- **Security**: PoA ensures high security through trusted validators, while DPoS adds an extra layer of security by enabling community oversight and active participation.
- **Decentralization**: DPoS brings decentralization to decision-making, as token holders have the power to elect delegates and influence the network’s governance.
- **Flexibility and Sustainability**: By combining PoA's efficiency with the community-led nature of DPoS, Pandora enables a highly flexible network that can evolve over time while maintaining sustainable growth.

This hybrid consensus model ensures that Pandora Chain remains both scalable and secure, providing a reliable platform for decentralized applications (dApps) while empowering the community to shape the network’s future.

## PDA Token

The PDA token is the native utility token of the Pandora Chain. It plays a central role in the network's operations and governance, enabling the ecosystem to function efficiently and in a decentralized manner. The PDA token serves two primary functions:

### 1. **Gas Token**
As the gas token of Pandora Chain, PDA is required to pay for transaction fees, smart contract execution, and other on-chain activities. Users and dApps must hold PDA tokens to interact with the blockchain, providing an essential utility to keep the network running smoothly. The cost of transactions is designed to be low, ensuring that the network remains affordable and accessible to a wide range of users and developers.

### 2. **Governance Token**
PDA also functions as the governance token of Pandora Chain, enabling holders to participate in the decision-making process for the network. Token holders can vote on community proposals, network upgrades, parameter adjustments, and other important governance decisions. This decentralized governance model ensures that the community has a direct say in the platform’s future, promoting transparency and inclusivity.

By using PDA for both transaction fees and governance, Pandora Chain aligns the interests of users, developers, and token holders, ensuring that the network evolves in a sustainable and community-driven way.

## Installation and Setup

To get started with Pandora Chain, you'll need to build the core binaries and set up your node. Below are the steps to guide you through the process, from building the source code to running a full node or validator.

### Prerequisites

Before you begin, ensure you have the following installed:

- **Go** (version 1.19 or higher)  
- **C Compiler** (for Go bindings)

These can typically be installed via your package manager (e.g., `apt`, `brew`, `yum`).

### Building Pandora Chain

To build the Pandora Chain client and associated utilities, clone the repository and run the build commands:

```bash
make geth
```

### Build the Full Suite of Utilities

If you want to build the full suite of utilities, including those used for development and node management, run:

```bash
make all
```

## Executables

This project includes several useful executables located in the `cmd/` directory. Below are some of the key tools you'll use:

| Command  | Description |
|----------|-------------|
| **geth** | Main Pandora Chain client binary. It runs the node and connects to the Pandora network. Supports full, archive, and light node modes. |
| **clef** | A standalone signing tool for use with the geth client, providing secure management of keys and signatures. |
| **devp2p** | Utilities to interact with network nodes at the protocol layer, useful for debugging and managing connections. |
| **abigen** | Code generator to convert Ethereum-style contract ABIs into Go packages for easier development. |
| **bootnode** | Lightweight tool to participate in node discovery without running the full client, ideal for creating bootstrap nodes. |
| **evm** | Developer utility for running Ethereum bytecode in an isolated environment, ideal for debugging and testing. |
| **rlpdump** | Utility to convert RLP (Recursive Length Prefix) encoded data into human-readable formats for debugging. |


## Running a Full Node or Validator
#### 1. Build the `geth` Client
After cloning the Pandora Chain repo and navigating to the project directory, build the `geth` client:
```
make geth
cp ./build/bin/geth /usr/local/bin/  # Move `geth` to a directory in your PATH
```

#### 2. Set Up Your Node Directory
The required configuration files are located in the `config/` folder. Copy them to your node directory:
```
mkdir -p ~/pda_node/data  # Create your node directory
cd ~/pda_node
cp ~/pdachain/config/* .  # Copy config files into your node directory
```

#### 3. Start the Node
To start a **full node**:
```
geth --pandora --config ./config.toml --datadir ./data
```
To start a **validator node**:

Prepare your wallet address and the associated keystore file.
Place your keystore file in `~/pda_node/data/keystore/`.
Then run the validator node with:
```
geth --pandora --config ./config.toml --datadir ./data --syncmode full -unlock <your_wallet_address> --password <password_file_for_keystore> --mine --allow-insecure-unlock --port 30311 --verbosity 5 --cache 18000
```

#### 4. Monitor Your Node
To monitor the status of your node, check the log file in `~/pda_node/data/pda.log` (or the specified log location). When the node starts syncing, you should see output like this:
```
t=2023-05-30T17:23:22+0000 lvl=info msg="Imported new chain segment" blocks=1 txs=0 mgas=0.000 elapsed="497.765µs"
```

#### 5. Interact with Your Node
To interact with your node using a built-in JavaScript console, start `geth` with the `console` command:
```
geth attach ipc://$HOME/pda_node/data/geth.ipc
```
This opens up a JavaScript environment where you can interact with the blockchain using Web3 methods and manage your node through Geth's APIs.

#### 6. Advanced Configuration
Instead of passing numerous flags directly, you can configure your node using a configuration file. This simplifies the process of passing parameters:
```
geth --config /path/to/your_config.toml
```
You can also export the current configuration using the `dumpconfig` command:
```
geth --your-favourite-flags dumpconfig
```

### Security Considerations
* **Keep test and mainnet accounts separate:** Always use different accounts for interacting with test networks and the main network. Pandora Chain will isolate these by default.

* **API Security:** Be cautious when enabling the HTTP or WebSocket interfaces. Exposing these APIs publicly can be a security risk. Ensure that you only allow trusted domains and connections.


## Interacting with Pandora Chain Programmatically

To interact with the Pandora Chain network programmatically, you can use its built-in JSON-RPC APIs, similar to how Ethereum operates. These APIs are accessible through HTTP, WebSockets (WS), or IPC (Inter-Process Communication), allowing flexibility depending on your application's needs. By default, the IPC interface is enabled, giving full access to the available APIs, while the HTTP and WebSocket interfaces are disabled for security reasons and must be manually enabled. These interfaces expose only a limited set of APIs to protect sensitive operations, but you can configure and manage them to suit your specific requirements, ensuring secure and efficient communication with Pandora Chain.

### Configuring JSON-RPC API Access

Below are the configuration options available for each transport:

#### HTTP-RPC Server Configuration

- `--http`  
  Enables the HTTP-RPC server.

- `--http.addr`  
  Specifies the HTTP-RPC server's listening interface (default: `localhost`).

- `--http.port`  
  Specifies the HTTP-RPC server's listening port (default: `8545`).

- `--http.api`  
  Defines the APIs available over the HTTP-RPC interface (default: `eth,net,web3`).

- `--http.corsdomain`  
  Defines the domains that are allowed to make cross-origin requests (CORS), separated by commas (browser-enforced).

#### WebSocket-RPC Server Configuration

- `--ws`  
  Enables the WebSocket-RPC server.

- `--ws.addr`  
  Specifies the WebSocket-RPC server's listening interface (default: `localhost`).

- `--ws.port`  
  Specifies the WebSocket-RPC server's listening port (default: `8546`).

- `--ws.api`  
  Defines the APIs available over the WebSocket-RPC interface (default: `eth,net,web3`).

- `--ws.origins`  
  Specifies the origins from which WebSocket requests are accepted.

#### IPC-RPC Server Configuration

- `--ipcdisable`  
  Disables the IPC-RPC server.

- `--ipcapi`  
  Defines the APIs available via the IPC interface (default: `admin,debug,eth,miner,net,personal,txpool,web3`).

- `--ipcpath`  
  Specifies the file path for the IPC socket/pipe within the data directory.

### Making Programmatic Calls

Once you've configured your node with the desired API transports, you'll need to connect using a suitable programming environment. Depending on your environment, you can use libraries or tools that support JSON-RPC communication over HTTP, WebSockets, or IPC. 

It is important to note that you can reuse a single connection for multiple requests, making it easier to interact with the node in a continuous manner.

### Security Considerations

Be cautious when exposing HTTP or WebSocket interfaces to the internet. Exposing these APIs increases the attack surface of your node, and malicious actors may attempt to exploit exposed services. It is essential to:

- **Limit access to trusted sources**: Consider restricting API access to specific domains, IPs, or networks. Use firewall rules or VPNs where appropriate.
- **Understand the risks of enabling public interfaces**: If you enable HTTP or WebSocket interfaces, hackers may attempt to subvert your node. Make sure to properly secure your environment before exposing these services.

### Final Thoughts

While Pandora Chain’s RPC interfaces make programmatic interactions convenient, opening these connections to the public can pose serious security risks. Always take the necessary precautions, and consider using private networks or VPNs for sensitive deployments.

## Contributing to Pandora Chain

Thank you for considering contributing to Pandora Chain! We appreciate any help, whether it's fixing a bug, improving documentation, or adding new features. Contributions from the community are vital to the growth and success of the project.

If you're interested in contributing, here’s how you can get involved:

1. **Fork the repository**: Start by forking the Pandora Chain project.
2. **Make changes**: Implement your changes or fixes in your forked repository.
3. **Commit your work**: Once you're happy with your changes, commit them to your fork.
4. **Submit a Pull Request**: Open a pull request (PR) to the main repository. One of our maintainers will review it and merge it if everything looks good.

For more substantial or architectural changes, we encourage you to reach out to the core development team via our Telegram channel before submitting your PR. This way, you can make sure your changes align with the project’s goals and philosophy, and we can provide early feedback to help streamline the process.

### Contribution Guidelines

To ensure consistency and quality in the codebase, please follow these guidelines:

- **Code Formatting**: Adhere to Go's official formatting standards. Ensure your code is formatted using `gofmt`.
- **Code Documentation**: Provide clear documentation for your code according to Go's commentary conventions.
- **Branching and Pull Requests**: Always base your pull request on the `main` branch. Make sure your PR is submitted against the `main` branch.
- **Commit Messages**: Prefix your commit messages with the relevant package(s) that were modified.  
  Example: `eth, rpc: make trace configs optional`

For additional details on setting up your development environment, managing dependencies, and testing your changes, please refer to this [Developer’s Guide](https://geth.ethereum.org/docs/developers).

## License

The Pandora Chain project is distributed under two different licenses, depending on the type of code:

- **Pandora Core Library**: The core library code (i.e., everything outside of the `cmd` directory) is licensed under the [GNU Lesser General Public License v3.0 (LGPL-3.0)](https://www.gnu.org/licenses/lgpl-3.0.html). A copy of this license can be found in the `COPYING.LESSER` file in the repository.

- **Pandora Command-Line Tools and Binaries**: The command-line tools and binaries (i.e., all code inside the `cmd` directory) are licensed under the [GNU General Public License v3.0 (GPL-3.0)](https://www.gnu.org/licenses/gpl-3.0.html). This license is also included in the repository in the `COPYING` file.

Please refer to the respective `COPYING` and `COPYING.LESSER` files for more details on the terms and conditions of the licenses.
