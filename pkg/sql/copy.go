// Copyright 2016 The Cockroach Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package sql

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"time"
	"unsafe"

	"github.com/cockroachdb/cockroach/pkg/internal/client"
	"github.com/cockroachdb/cockroach/pkg/sql/parser"
	"github.com/cockroachdb/cockroach/pkg/sql/pgwire/pgwirebase"
	"github.com/cockroachdb/cockroach/pkg/sql/privilege"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/types"
	"github.com/cockroachdb/cockroach/pkg/sql/sqlbase"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/cockroach/pkg/util/mon"
)

// copyMachine supports the Copy-in pgwire subprotocol (COPY...FROM STDIN). The
// machine is created by the Executor when that statement is executed; from that
// moment on, the machine takes control of the pgwire connection until
// copyMachine.run() returns. During this time, the machine is responsible for
// sending all the protocol messages (including the messages that are usually
// associated with statement results). Errors however are not sent on the
// connection by the machine; the higher layer is responsible for sending them.
//
// Incoming data is buffered and batched; batches are turned into insertNodes
// that are executed. INSERT privileges are required on the destination table.
//
// See: https://www.postgresql.org/docs/current/static/sql-copy.html
// and: https://www.postgresql.org/docs/current/static/protocol-flow.html#PROTOCOL-COPY
type copyMachine struct {
	table         tree.TableExpr
	columns       tree.NameList
	resultColumns sqlbase.ResultColumns
	buf           bytes.Buffer
	rows          []*tree.Tuple
	// insertedRows keeps track of the total number of rows inserted by the
	// machine.
	insertedRows int
	rowsMemAcc   mon.BoundAccount

	// conn is the pgwire connection from which data is to be read.
	conn pgwirebase.Conn

	// resetPlanner is a function to be used to prepare the planner for inserting
	// data.
	resetPlanner func(p *planner, txn *client.Txn, txnTS time.Time, stmtTS time.Time)

	txnOpt copyTxnOpt

	// p is the planner used to plan inserts. preparePlanner() needs to be called
	// before preparing each new statement.
	p planner

	// parsingEvalCtx is an EvalContext used for the very limited needs to strings
	// parsing. Is it not correcly initialized with timestamps, transactions and
	// other things that statements more generally need.
	parsingEvalCtx *tree.EvalContext
	// collationEnv is needed only when creating collated strings. Using a common
	// environment allows for some expensive work to only be done once.
	collationEnv tree.CollationEnvironment
}

// newCopyMachine creates a new copyMachine.
func newCopyMachine(
	ctx context.Context,
	conn pgwirebase.Conn,
	n *tree.CopyFrom,
	txnOpt copyTxnOpt,
	execCfg *ExecutorConfig,
	resetPlanner func(p *planner, txn *client.Txn, txnTS time.Time, stmtTS time.Time),
) (_ *copyMachine, retErr error) {
	c := &copyMachine{
		conn:    conn,
		table:   &n.Table,
		columns: n.Columns,
		txnOpt:  txnOpt,
		// The planner will be prepared before use.
		p:            planner{execCfg: execCfg},
		resetPlanner: resetPlanner,
	}
	c.resetPlanner(&c.p, nil /* txn */, time.Time{} /* txnTS */, time.Time{} /* stmtTS */)
	c.parsingEvalCtx = c.p.EvalContext()

	cleanup := c.preparePlanner(ctx)
	defer func() {
		retErr = cleanup(ctx, retErr)
	}()

	tn, err := n.Table.NormalizeWithDatabaseName(c.p.SessionData().Database)
	if err != nil {
		return nil, err
	}
	en, err := c.p.makeEditNode(ctx, tn, privilege.INSERT)
	if err != nil {
		return nil, err
	}
	cols, err := c.p.processColumns(en.tableDesc, n.Columns)
	if err != nil {
		return nil, err
	}
	c.resultColumns = make(sqlbase.ResultColumns, len(cols))
	for i, col := range cols {
		c.resultColumns[i] = sqlbase.ResultColumn{Typ: col.Type.ToDatumType()}
	}
	c.rowsMemAcc = c.p.extendedEvalCtx.Mon.MakeBoundAccount()
	return c, nil
}

// copyTxnOpt contains information about the transaction in which the copying
// should take place. Can be empty, in which case the copyMachine is responsible
// for managing its own transactions.
type copyTxnOpt struct {
	// If set, txn is the transaction within which all writes have to be
	// performed. Committing the txn is left to the higher layer.  If not set, the
	// machine will split writes between multiple transactions that it will
	// initiate.
	txn           *client.Txn
	txnTimestamp  time.Time
	stmtTimestamp time.Time
}

// run consumes all the copy-in data from the network connection and inserts it
// in the database.
func (c *copyMachine) run(ctx context.Context) (retErr error) {
	defer c.rowsMemAcc.Close(ctx)

	// Send the message describing the columns to the client.
	if err := c.conn.BeginCopyIn(ctx, c.resultColumns); err != nil {
		return err
	}

	// Read from the connection until we see an ClientMsgCopyDone.
	readBuf := pgwirebase.ReadBuffer{}

Loop:
	for {
		typ, _, err := readBuf.ReadTypedMsg(c.conn.Rd())
		if err != nil {
			return err
		}

		switch typ {
		case pgwirebase.ClientMsgCopyData:
			if err := c.processCopyData(
				ctx, string(readBuf.Msg), c.p.EvalContext(), false, /* final */
			); err != nil {
				return err
			}
		case pgwirebase.ClientMsgCopyDone:
			// If there's a line in the buffer without \n at EOL, add it here.
			if c.buf.Len() > 0 {
				if err := c.addRow(ctx, c.buf.Bytes()); err != nil {
					return err
				}
			}
			if err := c.processCopyData(
				ctx, "" /* data */, c.p.EvalContext(), true, /* final */
			); err != nil {
				return err
			}
			break Loop
		case pgwirebase.ClientMsgCopyFail:
			return fmt.Errorf("client canceled COPY")
		case pgwirebase.ClientMsgFlush, pgwirebase.ClientMsgSync:
			// Spec says to "ignore Flush and Sync messages received during copy-in mode".
		default:
			return pgwirebase.NewUnrecognizedMsgTypeErr(typ)
		}
	}

	// Finalize execution by sending the statement tag and number of rows
	// inserted.
	dummy := tree.CopyFrom{}
	tag := []byte(dummy.StatementTag())
	tag = append(tag, ' ')
	tag = strconv.AppendInt(tag, int64(c.insertedRows), 10 /* base */)
	return c.conn.SendCommandComplete(tag)
}

const (
	nullString = `\N`
	lineDelim  = '\n'
)

var (
	fieldDelim = []byte{'\t'}
)

// processCopyData buffers incoming data and, once the buffer fills up, inserts
// the accumulated rows.
//
// Args:
// final: If set, buffered data is written even if the buffer is not full.
func (c *copyMachine) processCopyData(
	ctx context.Context, data string, evalCtx *tree.EvalContext, final bool,
) error {
	// When this many rows are in the copy buffer, they are inserted.
	const copyBatchRowSize = 100

	c.buf.WriteString(data)
	for c.buf.Len() > 0 {
		line, err := c.buf.ReadBytes(lineDelim)
		if err != nil {
			if err != io.EOF {
				return err
			}
		} else {
			// Remove lineDelim from end.
			line = line[:len(line)-1]
			// Remove a single '\r' at EOL, if present.
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
		}
		if c.buf.Len() == 0 && bytes.Equal(line, []byte(`\.`)) {
			break
		}
		if err := c.addRow(ctx, line); err != nil {
			return err
		}
	}
	// Only do work if we have a full batch of rows or this is the end.
	if ln := len(c.rows); ln == 0 || (ln < copyBatchRowSize && !final) {
		return nil
	}
	return c.insertRows(ctx)
}

// preparePlanner resets the planner so that it can be used for execution.
// Depending on how the machine was configured, a new transaction might be
// created.
//
// It returns a cleanup function that needs to be called when we're done with
// the planner (before preparePlanner is called again). The cleanup function
// commits the txn (if it hasn't already been committed) or rolls it back
// depending on whether it is passed an error. If an error is passed in to the
// cleanup function, the same error is returned.
func (c *copyMachine) preparePlanner(ctx context.Context) func(context.Context, error) error {
	txn := c.txnOpt.txn
	txnTs := c.txnOpt.txnTimestamp
	stmtTs := c.txnOpt.stmtTimestamp
	autoCommit := false
	if txn == nil {
		txn = client.NewTxn(c.p.execCfg.DB, c.p.execCfg.NodeID.Get(), client.RootTxn)
		txnTs = c.p.execCfg.Clock.PhysicalTime()
		stmtTs = txnTs
		autoCommit = true
	}
	c.resetPlanner(&c.p, txn, txnTs, stmtTs)
	c.p.autoCommit = autoCommit

	return func(ctx context.Context, err error) error {
		if err == nil {
			// Ensure that the txn is committed if the copyMachine is in charge of
			// committing its transactions and the execution didn't already commit it
			// (through the planner.autoCommit optimization).
			if autoCommit && !txn.IsCommitted() {
				return txn.CommitOrCleanup(ctx)
			}
			return nil
		}
		txn.CleanupOnError(ctx, err)
		return err
	}
}

// insertRows transforms the buffered rows into an insertNode and executes it.
func (c *copyMachine) insertRows(ctx context.Context) (retErr error) {
	cleanup := c.preparePlanner(ctx)
	defer func() {
		retErr = cleanup(ctx, retErr)
	}()

	vc := &tree.ValuesClause{Tuples: c.rows}
	numRows := len(c.rows)
	// Reuse the same backing array once the Insert is complete.
	c.rows = c.rows[:0]
	c.rowsMemAcc.Clear(ctx)

	in := tree.Insert{
		Table:   c.table,
		Columns: c.columns,
		Rows: &tree.Select{
			Select: vc,
		},
		Returning: tree.AbsentReturningClause,
	}
	insertNode, err := c.p.Insert(ctx, &in, nil /* desiredTypes */)
	if err != nil {
		return err
	}
	defer insertNode.Close(ctx)

	params := runParams{
		ctx:             ctx,
		extendedEvalCtx: &c.p.extendedEvalCtx,
		p:               &c.p,
	}
	if err := startPlan(params, insertNode); err != nil {
		return err
	}
	rows, err := countRowsAffected(params, insertNode)
	if err != nil {
		return err
	}
	if rows != numRows {
		log.Fatalf(params.ctx, "didn't insert all buffered rows and yet no error was reported. "+
			"Inserted %d out of %d rows.", rows, numRows)
	}
	c.insertedRows += rows

	return nil
}

func (c *copyMachine) addRow(ctx context.Context, line []byte) error {
	var err error
	parts := bytes.Split(line, fieldDelim)
	if len(parts) != len(c.resultColumns) {
		return fmt.Errorf("expected %d values, got %d", len(c.resultColumns), len(parts))
	}
	exprs := make(tree.Exprs, len(parts))
	for i, part := range parts {
		s := string(part)
		if s == nullString {
			exprs[i] = tree.DNull
			continue
		}
		switch t := c.resultColumns[i].Typ; t {
		case types.Bytes,
			types.Date,
			types.Interval,
			types.INet,
			types.String,
			types.Timestamp,
			types.TimestampTZ,
			types.UUID:
			s, err = decodeCopy(s)
			if err != nil {
				return err
			}
		}
		d, err := parser.ParseStringAs(c.resultColumns[i].Typ, s, c.parsingEvalCtx, &c.collationEnv)
		if err != nil {
			return err
		}

		sz := d.Size()
		if err := c.rowsMemAcc.Grow(ctx, int64(sz)); err != nil {
			return err
		}

		exprs[i] = d
	}
	tuple := &tree.Tuple{Exprs: exprs}
	if err := c.rowsMemAcc.Grow(ctx, int64(unsafe.Sizeof(*tuple))); err != nil {
		return err
	}

	c.rows = append(c.rows, tuple)
	return nil
}

// decodeCopy unescapes a single COPY field.
//
// See: https://www.postgresql.org/docs/9.5/static/sql-copy.html#AEN74432
func decodeCopy(in string) (string, error) {
	var buf bytes.Buffer
	start := 0
	for i, n := 0, len(in); i < n; i++ {
		if in[i] != '\\' {
			continue
		}
		buf.WriteString(in[start:i])
		i++
		if i >= n {
			return "", fmt.Errorf("unknown escape sequence: %q", in[i-1:])
		}

		ch := in[i]
		if decodedChar := decodeMap[ch]; decodedChar != 0 {
			buf.WriteByte(decodedChar)
		} else if ch == 'x' {
			// \x can be followed by 1 or 2 hex digits.
			i++
			if i >= n {
				return "", fmt.Errorf("unknown escape sequence: %q", in[i-2:])
			}
			ch = in[i]
			digit, ok := decodeHexDigit(ch)
			if !ok {
				return "", fmt.Errorf("unknown escape sequence: %q", in[i-2:i])
			}
			if i+1 < n {
				if v, ok := decodeHexDigit(in[i+1]); ok {
					i++
					digit <<= 4
					digit += v
				}
			}
			buf.WriteByte(digit)
		} else if ch >= '0' && ch <= '7' {
			digit, _ := decodeOctDigit(ch)
			// 1 to 2 more octal digits follow.
			if i+1 < n {
				if v, ok := decodeOctDigit(in[i+1]); ok {
					i++
					digit <<= 3
					digit += v
				}
			}
			if i+1 < n {
				if v, ok := decodeOctDigit(in[i+1]); ok {
					i++
					digit <<= 3
					digit += v
				}
			}
			buf.WriteByte(digit)
		} else {
			return "", fmt.Errorf("unknown escape sequence: %q", in[i-1:i+1])
		}
		start = i + 1
	}
	buf.WriteString(in[start:])
	return buf.String(), nil
}

func decodeDigit(c byte, onlyOctal bool) (byte, bool) {
	switch {
	case c >= '0' && c <= '7':
		return c - '0', true
	case !onlyOctal && c >= '8' && c <= '9':
		return c - '0', true
	case !onlyOctal && c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case !onlyOctal && c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func decodeOctDigit(c byte) (byte, bool) { return decodeDigit(c, true) }
func decodeHexDigit(c byte) (byte, bool) { return decodeDigit(c, false) }

var decodeMap = map[byte]byte{
	'b':  '\b',
	'f':  '\f',
	'n':  '\n',
	'r':  '\r',
	't':  '\t',
	'v':  '\v',
	'\\': '\\',
}
