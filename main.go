package main

import (
	"log/slog"
	"os"

	"database/sql"

	"github.com/go-sql-driver/mysql"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/samott/web3scan/config"
	"github.com/samott/web3scan/scanner"
)

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

func handleEvent(event scanner.Event, db *sql.Tx) (error) {
	slog.Info("Handling event...");
	return nil;
}

func main() {
	slog.Info("Running...");

	cfg, err := config.LoadConfig("./web3scan.yaml");

	if err != nil {
		slog.Error("Error loading config file", "error", err);
		return;
	}

	dbConfig := mysql.Config{
		User: cfg.Database.User,
		DBName: cfg.Database.DBName,
		Addr: cfg.Database.Addr,
		AllowNativePasswords: true,
	};

	db, err := sql.Open("mysql", dbConfig.FormatDSN());

	if err != nil {
		slog.Error("Error connecting to database", "error", err);
		return;
	}

	defer db.Close();

	abis, err := loadAbis(cfg.Contracts);

	scan := scanner.New(
		cfg,
		db,
		abis,
		handleEvent,
	);

	scan.Run();
}
