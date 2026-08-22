package pathdb

import (
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

// HistoricalStateReader is a wrapper over history reader, providing access to
// historical state.
type historicalStateReader struct {
	db     *Database
	reader *historyReader
	id     uint64
}

// HistoricReader constructs a reader for accessing the requested historic
// state. A single PathDB is exposed through the same public wrapper used by
// ParaDatabase, with one underlying shard reader.
func (db *Database) HistoricReader(root common.Hash) (*HistoricalStateReader, error) {
	reader, err := db.historicReader(root)
	if err != nil {
		return nil, err
	}
	return &HistoricalStateReader{stateReaders: []*historicalStateReader{reader}}, nil
}

// historicReader constructs the shard-local historical reader.
func (db *Database) historicReader(root common.Hash) (*historicalStateReader, error) {
	if db.stateIndexer == nil || db.stateFreezer == nil {
		return nil, fmt.Errorf("historical state %x is not available", root)
	}
	if !db.stateIndexer.inited() {
		return nil, errors.New("state histories haven't been fully indexed yet")
	}
	id := rawdb.ReadStateID(db.diskdb, root)
	if id == nil {
		return nil, fmt.Errorf("state %#x is not available", root)
	}
	meta, err := readStateHistoryMeta(db.stateFreezer, *id+1)
	if err != nil {
		return nil, err
	}
	if meta.parent != root {
		return nil, fmt.Errorf("state %#x is not canonical", root)
	}
	return &historicalStateReader{
		id:     *id,
		db:     db,
		reader: newHistoryReader(db.diskdb, db.stateFreezer),
	}, nil
}

// AccountRLP directly retrieves the account RLP associated with a particular
// address in the slim data format. An error will be returned if the read
// operation exits abnormally. Specifically, if the layer is already stale.
//
// Note:
// - the returned account is not a copy, please don't modify it.
// - no error will be returned if the requested account is not found in database.
func (r *historicalStateReader) accountRLP(address common.Address) ([]byte, error) {
	defer func(start time.Time) {
		historicalAccountReadTimer.UpdateSince(start)
	}(time.Now())

	// TODO(rjl493456442): Theoretically, the obtained disk layer could become stale
	// within a very short time window.
	//
	// While reading the account data while holding `db.tree.lock` can resolve
	// this issue, but it will introduce a heavy contention over the lock.
	//
	// Let's optimistically assume the situation is very unlikely to happen,
	// and try to define a low granularity lock if the current approach doesn't
	// work later.
	dl := r.db.tree.bottom()
	hash := crypto.Keccak256Hash(address.Bytes())
	latest, err := dl.account(hash, 0)
	if err != nil {
		return nil, err
	}
	return r.reader.read(newAccountIdentQuery(address, hash), r.id, dl.stateID(), latest)
}

// Account directly retrieves the account associated with a particular address in
// the slim data format. An error will be returned if the read operation exits
// abnormally. Specifically, if the layer is already stale.
//
// No error will be returned if the requested account is not found in database
func (r *historicalStateReader) account(address common.Address) (*types.SlimAccount, error) {
	blob, err := r.accountRLP(address)
	if err != nil {
		return nil, err
	}
	if len(blob) == 0 {
		return nil, nil
	}
	account := new(types.SlimAccount)
	if err := rlp.DecodeBytes(blob, account); err != nil {
		panic(err)
	}
	return account, nil
}

// Storage directly retrieves the storage data associated with a particular key,
// within a particular account. An error will be returned if the read operation
// exits abnormally. Specifically, if the layer is already stale.
//
// Note:
// - the returned storage data is not a copy, please don't modify it.
// - no error will be returned if the requested slot is not found in database.
func (r *historicalStateReader) storage(address common.Address, key common.Hash) ([]byte, error) {
	defer func(start time.Time) {
		historicalStorageReadTimer.UpdateSince(start)
	}(time.Now())

	// TODO(rjl493456442): Theoretically, the obtained disk layer could become stale
	// within a very short time window.
	//
	// While reading the account data while holding `db.tree.lock` can resolve
	// this issue, but it will introduce a heavy contention over the lock.
	//
	// Let's optimistically assume the situation is very unlikely to happen,
	// and try to define a low granularity lock if the current approach doesn't
	// work later.
	dl := r.db.tree.bottom()
	addrHash := crypto.Keccak256Hash(address.Bytes())
	keyHash := crypto.Keccak256Hash(key.Bytes())
	latest, err := dl.storage(addrHash, keyHash, 0)
	if err != nil {
		return nil, err
	}
	return r.reader.read(newStorageIdentQuery(address, addrHash, key, keyHash), r.id, dl.stateID(), latest)
}
