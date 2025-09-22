package pkg

import (
	"context"
	"fmt"
	"os"

	"github.com/c2h5oh/datasize"
	mdbx2 "github.com/erigontech/mdbx-go/mdbx"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"

	logv3 "github.com/ledgerwatch/log/v3"
)

// OpenChainDB opens chaindata database
func OpenChainDB(dbPath string, log logv3.Logger) (kv.RwDB, error) {
	ctx := context.Background()
	opts := mdbx.NewMDBX(log).Path(dbPath).Label(kv.ChainDB).WithTableCfg(mdbx.WithChaindataTables)

	// Get database info
	env, err := mdbx2.NewEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create env: %v", err)
	}
	err = env.Open(dbPath, opts.GetFlags(), 0664)
	if err != nil {
		return nil, fmt.Errorf("failed to open env: %v", err)
	}
	in, err := env.Info(nil)
	if err != nil {
		env.Close()
		return nil, fmt.Errorf("failed to get env info: %v", err)
	}
	env.Close()

	newMapSize := datasize.ByteSize(in.MapSize)
	log.Info("Database info", "pageSize", in.PageSize, "mapSize", newMapSize.TBytes(), "flags", in.Flags)

	// Open database
	chaindb, err := opts.Flags(func(flags uint) uint {
		newFlags := int(in.Flags)
		newFlags &= ^mdbx2.Readonly
		newFlags |= mdbx2.WriteMap
		return uint(newFlags)
	}).PageSize(uint64(in.PageSize)).MapSize(newMapSize).Open(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to open chaindata db: %v", err)
	}

	return chaindb, nil
}

// GetTableNames gets all table names in database
func GetTableNames(db kv.RwDB) ([]string, error) {
	ctx := context.Background()
	tx, err := db.BeginRo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start read transaction: %v", err)
	}
	defer tx.Rollback()

	tables := make([]string, 0)
	err = tx.ForEach("", []byte{}, func(k, v []byte) error {
		tables = append(tables, string(k))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate tables: %v", err)
	}

	return tables, nil
}

// CheckDBPath checks if database path exists
func CheckDBPath(dbPath string) error {
	if _, err := os.Stat(dbPath + "/mdbx.dat"); os.IsNotExist(err) {
		return fmt.Errorf("database path does not exist: %s/mdbx.dat", dbPath)
	}
	return nil
}
