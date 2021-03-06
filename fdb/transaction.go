// FoundationDB Go API
// Copyright (c) 2013 FoundationDB, LLC

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package fdb

/*
 #define FDB_API_VERSION 100
 #include <foundationdb/fdb_c.h>
*/
import "C"

import (
	"runtime"
)

// A ReadTransaction represents an object that can asynchronously read from a
// FoundationDB database. Transaction and Snapshot both satisfy the
// ReadTransaction interface.
type ReadTransaction interface {
	Get(key KeyConvertible) FutureValue
	GetKey(sel Selectable) FutureKey
	GetRange(r Range, options RangeOptions) RangeResult
	GetReadVersion() FutureVersion
	GetDatabase() Database
	LocalityGetAddressesForKey(key KeyConvertible) FutureStringArray
}

// Transaction is a handle to a FoundationDB transaction. Transaction is a
// lightweight object that may be efficiently copied, and is safe for concurrent
// use by multiple goroutines.
//
// In FoundationDB, a transaction is a mutable snapshot of a database. All read
// and write operations on a transaction see and modify an otherwise-unchanging
// version of the database and only change the underlying database if and when
// the transaction is committed. Read operations do see the effects of previous
// write operations on the same transaction. Committing a transaction usually
// succeeds in the absence of conflicts.
//
// Transactions group operations into a unit with the properties of atomicity,
// isolation, and durability. Transactions also provide the ability to maintain
// an application’s invariants or integrity constraints, supporting the property
// of consistency. Together these properties are known as ACID.
//
// Transactions are also causally consistent: once a transaction has been
// successfully committed, all subsequently created transactions will see the
// modifications made by it.
type Transaction struct {
	*transaction
}

type transaction struct {
	ptr *C.FDBTransaction
	db Database
}

// TransactionOptions is a handle with which to set options that affect a
// Transaction object. A TransactionOptions instance should be obtained with the
// (Transaction).Options() method.
type TransactionOptions struct {
	transaction *transaction
}

func (opt TransactionOptions) setOpt(code int, param []byte) error {
	return setOpt(func(p *C.uint8_t, pl C.int) C.fdb_error_t {
		return C.fdb_transaction_set_option(opt.transaction.ptr, C.FDBTransactionOption(code), p, pl)
	}, param)
}

func (t *transaction) destroy() {
	C.fdb_transaction_destroy(t.ptr)
}

// GetDatabase returns a handle to the database with which this transaction is
// interacting.
func (t Transaction) GetDatabase() Database {
	return t.transaction.db
}

// Transact passes the Transaction receiver object to the caller-provided
// function, but does not handle errors or commit the transaction.
//
// Transact makes Transaction satisfy the Transactor interface, allowing
// transactional functions to be used compositionally.
//
// See the Transactor interface for an example of using Transact with
// Transaction and Database objects.
func (t Transaction) Transact(f func (tr Transaction) (interface{}, error)) (interface{}, error) {
	return f(t)
}

// Cancel cancels a transaction. All pending or future uses of the transaction
// will encounter an error. The Transaction object may be reused after calling
// (Transaction).Reset().
//
// Be careful if you are using (Transaction).Reset() and (Transaction).Cancel()
// concurrently with the same transaction. Since they negate each other’s
// effects, a race condition between these calls will leave the transaction in
// an unknown state.
//
// If your program attempts to cancel a transaction after (Transaction).Commit()
// has been called but before it returns, unpredictable behavior will
// result. While it is guaranteed that the transaction will eventually end up in
// a cancelled state, the commit may or may not occur. Moreover, even if the
// call to (Transaction).Commit() appears to return a transaction_cancelled
// error, the commit may have occurred or may occur in the future. This can make
// it more difficult to reason about the order in which transactions occur.
func (t Transaction) Cancel() {
	C.fdb_transaction_cancel(t.ptr)
}

// (Infrequently used) SetReadVersion sets the database version that the transaction will read from
// the database. The database cannot guarantee causal consistency if this method
// is used (the transaction’s reads will be causally consistent only if the
// provided read version has that property).
func (t Transaction) SetReadVersion(version int64) {
	C.fdb_transaction_set_read_version(t.ptr, C.int64_t(version))
}

// Snapshot returns a Snapshot object, suitable for performing snapshot
// reads. Snapshot reads offer a more relaxed isolation level than
// FoundationDB's default serializable isolation, reducing transaction conflicts
// but making it harder to reason about concurrency.
//
// For more information on snapshot reads, see
// https://foundationdb.com/documentation/developer-guide.html#using-snapshot-reads.
func (t Transaction) Snapshot() Snapshot {
	return Snapshot{t.transaction}
}

func makeFutureNil(fp *C.FDBFuture) FutureNil {
	f := &future{fp}
	runtime.SetFinalizer(f, (*future).destroy)
	return FutureNil{f}
}

// OnError determines whether an error returned by a Transaction method is
// retryable. Waiting on the returned future will return the same error when
// fatal, or return nil (after blocking the calling goroutine for a suitable
// delay) for retryable errors.
//
// Typical code will not use OnError directly. (Database).Transact() uses
// OnError internally to implement a correct retry loop.
func (t Transaction) OnError(e Error) FutureNil {
	return makeFutureNil(C.fdb_transaction_on_error(t.ptr, C.fdb_error_t(e)))
}

// Commit attempts to commit the modifications made in the transaction to the
// database. Waiting on the returned future will block the calling goroutine
// until the transaction has either been committed successfully or an error is
// encountered. Any error should be passed to (Transaction).OnError() to determine
// if the error is retryable or not.
//
// As with other client/server databases, in some failure scenarios a client may
// be unable to determine whether a transaction succeeded. For more information,
// see
// https://foundationdb.com/documentation/developer-guide.html#developer-guide-unknown-results.
func (t Transaction) Commit() FutureNil {
	return makeFutureNil(C.fdb_transaction_commit(t.ptr))
}

// Watch creates a watch and returns a FutureNil that will become ready when the
// watch reports a change to the value of the specified key.
//
// A watch’s behavior is relative to the transaction that created it. A watch
// will report a change in relation to the key’s value as readable by that
// transaction. The initial value used for comparison is either that of the
// transaction’s read version or the value as modified by the transaction itself
// prior to the creation of the watch. If the value changes and then changes
// back to its initial value, the watch might not report the change.
//
// Until the transaction that created it has been committed, a watch will not
// report changes made by other transactions. In contrast, a watch will
// immediately report changes made by the transaction itself. Watches cannot be
// created if the transaction has set
// (Transaction).Options().SetReadYourWritesDisable(), and an attempt to do so
// will return a watches_disabled error.
//
// By default, each database connection can have no more than 10,000 watches
// that have not yet reported a change. When this number is exceeded, an attempt
// to create a watch will return a too_many_watches error. This limit can be
// changed using (Database).Options().SetMaxWatches(). Because a watch outlives
// the transaction that creates it, any watch that is no longer needed should be
// cancelled by calling (FutureNil).Cancel() on its returned future.
func (t Transaction) Watch(key KeyConvertible) FutureNil {
	kb := key.ToFDBKey()
	return makeFutureNil(C.fdb_transaction_watch(t.ptr, byteSliceToPtr(kb), C.int(len(kb))))
}

func (t *transaction) get(key []byte, snapshot int) FutureValue {
	f := &future{C.fdb_transaction_get(t.ptr, byteSliceToPtr(key), C.int(len(key)), C.fdb_bool_t(snapshot))}
	runtime.SetFinalizer(f, (*future).destroy)
	return FutureValue{&futureValue{future: f}}
}

// Get returns the (future) value associated with the specified key. The read is
// performed asynchronously and does not block the calling goroutine. The future
// will become ready when the read is complete.
func (t Transaction) Get(key KeyConvertible) FutureValue {
	return t.get(key.ToFDBKey(), 0)
}

func (t *transaction) doGetRange(r Range, options RangeOptions, snapshot bool, iteration int) futureKeyValueArray {
	begin := r.BeginKeySelector()
	bkey := begin.Key.ToFDBKey()
	end := r.EndKeySelector()
	ekey := end.Key.ToFDBKey()
	f := &future{C.fdb_transaction_get_range(t.ptr, byteSliceToPtr(bkey), C.int(len(bkey)), C.fdb_bool_t(boolToInt(begin.OrEqual)), C.int(begin.Offset), byteSliceToPtr(ekey), C.int(len(ekey)), C.fdb_bool_t(boolToInt(end.OrEqual)), C.int(end.Offset), C.int(options.Limit), C.int(0), C.FDBStreamingMode(options.Mode-1), C.int(iteration), C.fdb_bool_t(boolToInt(snapshot)), C.fdb_bool_t(boolToInt(options.Reverse)))}
	runtime.SetFinalizer(f, (*future).destroy)
	return futureKeyValueArray{f}
}

func (t *transaction) getRange(r Range, options RangeOptions, snapshot bool) RangeResult {
	f := t.doGetRange(r, options, snapshot, 1)
	return RangeResult{
		t: t,
		sr: SelectorRange{r.BeginKeySelector(), r.EndKeySelector()},
		options: options,
		snapshot: snapshot,
		f: &f,
	}
}

// GetRange performs a range read. The returned RangeResult represents all
// KeyValue objects kv where beginKey <= kv.Key < endKey, ordered by
// kv.Key. beginKey and endKey are the keys described by the key selectors
// r.BeginKeySelector() and r.EndKeySelector(). All reads performed as a result
// of GetRange are asynchronous and do not block the calling goroutine.
func (t Transaction) GetRange(r Range, options RangeOptions) RangeResult {
	return t.getRange(r, options, false)
}

func (t *transaction) getReadVersion() FutureVersion {
	f := &future{C.fdb_transaction_get_read_version(t.ptr)}
	runtime.SetFinalizer(f, (*future).destroy)
	return FutureVersion{f}
}

// (Infrequently used) GetReadVersion returns the (future) transaction read version. The read is
// performed asynchronously and does not block the calling goroutine. The future
// will become ready when the read version is available.
func (t Transaction) GetReadVersion() FutureVersion {
	return t.getReadVersion()
}

// Set associated the given key and value, overwriting any previous association
// with key. Set returns immediately, having modified the snapshot of the
// database represented by the transaction.
func (t Transaction) Set(key KeyConvertible, value []byte) {
	kb := key.ToFDBKey()
	C.fdb_transaction_set(t.ptr, byteSliceToPtr(kb), C.int(len(kb)), byteSliceToPtr(value), C.int(len(value)))
}

// Clear removes the specified key (and any associated value), if it
// exists. Clear returns immediately, having modified the snapshot of the
// database represented by the transaction.
func (t Transaction) Clear(key KeyConvertible) {
	kb := key.ToFDBKey()
	C.fdb_transaction_clear(t.ptr, byteSliceToPtr(kb), C.int(len(kb)))
}

// ClearRange removes all keys k such that begin <= k < end, and their
// associated values. ClearRange returns immediately, having modified the
// snapshot of the database represented by the transaction.
func (t Transaction) ClearRange(er ExactRange) {
	bkb := er.BeginKey().ToFDBKey()
	ekb := er.EndKey().ToFDBKey()
	C.fdb_transaction_clear_range(t.ptr, byteSliceToPtr(bkb), C.int(len(bkb)), byteSliceToPtr(ekb), C.int(len(ekb)))
}

// (Infrequently used) GetCommittedVersion returns the version number at which a successful commit
// modified the database. This must be called only after the successful
// (non-error) completion of a call to (Transaction).Commit() on this
// Transaction, or the behavior is undefined. Read-only transactions do not
// modify the database when committed and will have a committed version of
// -1. Keep in mind that a transaction which reads keys and then sets them to
// their current values may be optimized to a read-only transaction.
func (t Transaction) GetCommittedVersion() (int64, error) {
	var version C.int64_t

	if err := C.fdb_transaction_get_committed_version(t.ptr, &version); err != 0 {
		return 0, Error(err)
	}

	return int64(version), nil
}

// Reset rolls back a transaction, completely resetting it to its initial
// state. This is logically equivalent to destroying the transaction and
// creating a new one.
func (t Transaction) Reset() {
	C.fdb_transaction_reset(t.ptr)
}

func boolToInt(b bool) int {
	if b {
		return 1
	} else {
		return 0
	}
}

func (t *transaction) getKey(sel KeySelector, snapshot int) FutureKey {
	key := sel.Key.ToFDBKey()
	f := &future{C.fdb_transaction_get_key(t.ptr, byteSliceToPtr(key), C.int(len(key)), C.fdb_bool_t(boolToInt(sel.OrEqual)), C.int(sel.Offset), C.fdb_bool_t(snapshot))}
	runtime.SetFinalizer(f, (*future).destroy)
	return FutureKey{&futureKey{future: f}}
}

// GetKey returns the future key referenced by the provided key selector. The
// read is performed asynchronously and does not block the calling
// goroutine. The future will become ready when the read version is available.
func (t Transaction) GetKey(sel Selectable) FutureKey {
	return t.getKey(sel.ToFDBKeySelector(), 0)
}

func (t Transaction) atomicOp(key []byte, param []byte, code int) {
	C.fdb_transaction_atomic_op(t.ptr, byteSliceToPtr(key), C.int(len(key)), byteSliceToPtr(param), C.int(len(param)), C.FDBMutationType(code))
}

func addConflictRange(t *transaction, er ExactRange, crtype conflictRangeType) error {
	begin := er.BeginKey().ToFDBKey()
	end := er.EndKey().ToFDBKey()
	if err := C.fdb_transaction_add_conflict_range(t.ptr, byteSliceToPtr(begin), C.int(len(begin)), byteSliceToPtr(end), C.int(len(end)), C.FDBConflictRangeType(crtype)); err != 0 {
		return Error(err)
	}

	return nil
}

// AddReadConflictRange adds a range of keys to the transaction’s read conflict
// ranges as if you had read the range. As a result, other transactions that
// write a key in this range could cause the transaction to fail with a
// conflict.
//
// For more information on conflict ranges, see
// https://foundationdb.com/documentation/developer-guide.html#conflict-ranges.
func (t Transaction) AddReadConflictRange(er ExactRange) error {
	return addConflictRange(t.transaction, er, conflictRangeTypeRead)
}

func copyAndAppend(orig []byte, b byte) []byte {
	ret := make([]byte, len(orig) + 1)
	copy(ret, orig)
	return append(ret, b)
}

// AddReadConflictKey adds a key to the transaction’s read conflict ranges as if
// you had read the key. As a result, other transactions that concurrently write
// this key could cause the transaction to fail with a conflict.
//
// For more information on conflict ranges, see
// https://foundationdb.com/documentation/developer-guide.html#conflict-ranges.
func (t Transaction) AddReadConflictKey(key KeyConvertible) error {
	return addConflictRange(t.transaction, KeyRange{key, Key(copyAndAppend(key.ToFDBKey(), 0x00))}, conflictRangeTypeRead)
}

// AddWriteConflictRange adds a range of keys to the transaction’s write
// conflict ranges as if you had cleared the range. As a result, other
// transactions that concurrently read a key in this range could fail with a
// conflict.
//
// For more information on conflict ranges, see
// https://foundationdb.com/documentation/developer-guide.html#conflict-ranges.
func (t Transaction) AddWriteConflictRange(er ExactRange) error {
	return addConflictRange(t.transaction, er, conflictRangeTypeWrite)
}

// AddWriteConflictKey adds a key to the transaction’s write conflict ranges as
// if you had written the key. As a result, other transactions that concurrently
// read this key could fail with a conflict.
//
// For more information on conflict ranges, see
// https://foundationdb.com/documentation/developer-guide.html#conflict-ranges.
func (t Transaction) AddWriteConflictKey(key KeyConvertible) error {
	return addConflictRange(t.transaction, KeyRange{key, Key(copyAndAppend(key.ToFDBKey(), 0x00))}, conflictRangeTypeWrite)
}

// Options returns a TransactionOptions instance suitable for setting options
// specific to this transaction.
func (t Transaction) Options() TransactionOptions {
	return TransactionOptions{t.transaction}
}

func localityGetAddressesForKey(t *transaction, key KeyConvertible) FutureStringArray {
	kb := key.ToFDBKey()

	f := &future{C.fdb_transaction_get_addresses_for_key(t.ptr, byteSliceToPtr(kb), C.int(len(kb)))}
	runtime.SetFinalizer(f, (*future).destroy)
	return FutureStringArray{f}
}

// LocalityGetAddressesForKey returns the (future) public network addresses of
// each of the storage servers responsible for storing key and its associated
// value. The read is performed asynchronously and does not block the calling
// goroutine. The future will become ready when the read is complete.
func (t Transaction) LocalityGetAddressesForKey(key KeyConvertible) FutureStringArray {
	return localityGetAddressesForKey(t.transaction, key)
}

// Snapshot is a handle to a FoundationDB transaction snapshot, suitable for
// performing snapshot reads. Snapshot reads offer a more relaxed isolation
// level than FoundationDB's default serializable isolation, reducing
// transaction conflicts but making it harder to reason about concurrency.
//
// For more information on snapshot reads, see
// https://foundationdb.com/documentation/developer-guide.html#snapshot-reads.
type Snapshot struct {
	*transaction
}

// Like (Transaction).Get(), but as a snapshot read.
func (s Snapshot) Get(key KeyConvertible) FutureValue {
	return s.get(key.ToFDBKey(), 1)
}

// Like (Transaction).GetKey(), but as a snapshot read.
func (s Snapshot) GetKey(sel Selectable) FutureKey {
	return s.getKey(sel.ToFDBKeySelector(), 1)
}

// Like (Transaction).GetRange(), but as a snapshot read.
func (s Snapshot) GetRange(r Range, options RangeOptions) RangeResult {
	return s.getRange(r, options, true)
}

// Like (Transaction).GetReadVersion(), but as a snapshot read.
func (s Snapshot) GetReadVersion() FutureVersion {
	return s.getReadVersion()
}

// GetDatabase returns a handle to the database with which this snapshot is
// interacting.
func (s Snapshot) GetDatabase() Database {
	return s.transaction.db
}

// LocalityGetAddressesForKey returns the (future) public network addresses of
// each of the storage servers responsible for storing key and its associated
// value. The read is performed asynchronously and does not block the calling
// goroutine. The future will become ready when the read is complete.
func (s Snapshot) LocalityGetAddressesForKey(key KeyConvertible) FutureStringArray {
	return localityGetAddressesForKey(s.transaction, key)
}
