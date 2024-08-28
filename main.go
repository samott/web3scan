package main

import (
	"strings"
	"errors"
	"log/slog"
	"os"

	"database/sql"

	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/samott/web3scan/config"
	"github.com/samott/web3scan/scanner"
)

var (
	ErrMissingEventArgument = errors.New("Missing event argument");
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

func handleEvent(event *scanner.Event, tx *sql.Tx) (error) {
	// Custom handler for ERC20 tranfers

	slog.Info("Handling event...");

	if event.Event != "Transfer" {
		return nil;
	}

	from, exists := event.Args["from"];

	if !exists {
		return ErrMissingEventArgument;
	}

	to, exists := event.Args["to"];

	if !exists {
		return ErrMissingEventArgument;
	}

	amount, exists := event.Args["amount"];

	if !exists {
		return ErrMissingEventArgument;
	}

	type CurrencyDef struct {
		id string;
		decimals int32;
	}

	tokens := map[string]CurrencyDef{
		"0x7ceb23fd6bc0add59e62ac25578270cff1b9f619": {
			id: "eth",
			decimals: 18,
		},
		"0x3c499c542cef5e3811e1192ce70d8cc03d5c3359": {
			id: "usdc",
			decimals: 6,
		},
	};

	// These should be lowercase
	users := map[string]bool{
		"0x1111111111111111111111111111111111111111": true,
		"0x2222222222222222222222222222222222222222": true,
	};

	currency, exists := tokens[event.Contract];

	if !exists {
		return errors.New("Event for unknown ERC20 contract");
	}

	scale := decimal.NewFromInt(10).Pow(decimal.NewFromInt(int64(currency.decimals)))

	amountDec, err := decimal.NewFromString(amount.(string));

	if err != nil {
		return err;
	}

	amountStr := amountDec.Div(scale).StringFixed(currency.decimals);

	fromStr := strings.ToLower(from.(string));
	toStr := strings.ToLower(to.(string));

	var user string;

	if _, exists := users[fromStr]; exists {
		user = fromStr;
	} else if _, exists := users[toStr]; exists {
		user = toStr;
	} else {
		// Not one of our users - don't track
		return nil;
	}

	slog.Info(
		"Updating user balance...",
		"user",
		user,
		"currency",
		currency.id,
		"amount",
		amountStr,
	);

	_, err = tx.Exec(`
		UPDATE balances
		SET balance = balance + CAST(? AS Decimal(32, 18))
		WHERE wallet = ?
		AND currency = ?
		AND (balance - ?) >= 0
	`, user, currency.id, amountStr);

	return err;
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
