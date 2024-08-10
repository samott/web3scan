package main

import (
	"context"
	"log/slog"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/samott/web3scan/config"
)

var abis map[string]*abi.ABI;

func scanBlocks(
	contract string,
	rpcUrl string,
	abi *abi.ABI,
	startBlock uint64,
	endBlock uint64,
) {
	client, err := ethclient.Dial(rpcUrl);

	if err != nil {
		slog.Error("Failed to connect to RPC node", "error", err);
		return;
	}

	contractAddress := common.HexToAddress(contract);
	eventSignature := []byte("Transfer(address,address,uint256)");
	eventSignatureHash := crypto.Keccak256Hash(eventSignature);

	startBlockBig := big.NewInt(int64(startBlock));
	endBlockBig := big.NewInt(int64(endBlock));

	query := ethereum.FilterQuery{
		FromBlock: startBlockBig,
		ToBlock:   endBlockBig,
		Addresses: []common.Address{contractAddress},
		Topics:    [][]common.Hash{{eventSignatureHash}},
	}

	logs, err := client.FilterLogs(context.Background(), query)

	if err != nil {
		slog.Error("Error filtering logs", "error", err);
		return;
	}

	for _, log := range logs {
		slog.Info("Event", "block", log.BlockNumber, "index", log.Index);

		data := map[string]any{};
		eventHash := log.Topics[0];

		eventAbi, err := abi.EventByID(eventHash);

		if err != nil {
			slog.Error("Error getting event ABI", "error", err);
			return;
		}

		gotArgs := 0;
		argCount := len(eventAbi.Inputs);

		// Decode the indexed arguments
		for i := 0; i < argCount; i++ {
			if !eventAbi.Inputs[i].Indexed {
				break;
			}

			argName := eventAbi.Inputs[i].Name;
			data[argName] = log.Topics[i+1]; // +1 to skip event hash @ topic 0
			gotArgs++;
		}

		// Decode the remaining arguments
		if gotArgs < argCount {
			unindexed := map[string]any{};

			err = abi.UnpackIntoMap(unindexed, eventAbi.Name, log.Data);

			if err != nil {
				slog.Error("Error getting event ABI", "error", err);
				return;
			}

			for name, value := range unindexed {
				data[name] = value;
			}
		}

		slog.Info("Event", "event", data);
    }
}

func loadAbis(contracts []config.Contract) (map[string]*abi.ABI, error) {
	abis := map[string]*abi.ABI{};
	files := map[string]*abi.ABI{};

	for i := 0; i < len(contracts); i++ {
		path := contracts[i].AbiPath;

		if _, exists := files[path]; exists {
			abis[contracts[i].Address] = files[path];
			continue;
		}

		file, err := os.Open(path);

		if err != nil {
			return abis, err;
		}

		contractAbi, err := abi.JSON(file);

		if err != nil {
			return abis, err;
		}

		abis[contracts[i].Address] = &contractAbi;
		files[path] = &contractAbi;

		file.Close();
	}

	return abis, nil;
}

func main() {
	slog.Info("Running...");

	cfg, err := config.LoadConfig("./web3scan.yaml");

	if err != nil {
		slog.Error("Error loading config file", "error", err);
		return;
	}

	abis, err := loadAbis(cfg.Contracts);

	if err != nil {
		slog.Error("Error loading ABI", "error", err);
		return;
	}

	contract := cfg.Contracts[0];

	start := uint64(contract.StartBlock);

	scanBlocks(
		contract.Address,
		cfg.RpcNodes[0],
		abis[contract.Address],
		start,
		start + uint64(cfg.BlocksPerRequest),
	);
}
