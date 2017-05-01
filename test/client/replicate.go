package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flimzy/kivik"
	"github.com/flimzy/kivik/test/kt"
)

func init() {
	kt.Register("Replicate", replicate)
}

func replicate(ctx *kt.Context) {
	defer lockReplication(ctx)()
	ctx.RunRW(func(ctx *kt.Context) {
		ctx.RunAdmin(func(ctx *kt.Context) {
			testReplication(ctx, ctx.Admin)
		})
		ctx.RunNoAuth(func(ctx *kt.Context) {
			testReplication(ctx, ctx.NoAuth)
		})
	})
}

func callReplicate(ctx *kt.Context, client *kivik.Client, target, source, repID string, opts kivik.Options) (*kivik.Replication, error) {
	opts = replicationOptions(ctx, client, target, source, repID, opts)
	return client.Replicate(context.Background(), target, source, opts)
}

func testReplication(ctx *kt.Context, client *kivik.Client) {
	prefix := ctx.String("prefix")
	switch prefix {
	case "":
		prefix = strings.TrimSuffix(client.DSN(), "/") + "/"
	case "none":
		prefix = ""
	}
	dbtarget := prefix + ctx.TestDB()
	dbsource := prefix + ctx.TestDB()
	defer ctx.Admin.DestroyDB(context.Background(), dbtarget, ctx.Options("db"))
	defer ctx.Admin.DestroyDB(context.Background(), dbsource, ctx.Options("db"))

	db, err := ctx.Admin.DB(context.Background(), dbsource)
	if err != nil {
		ctx.Fatalf("Failed to open db: %s", err)
	}

	// Create 10 docs for testing sync
	for i := 0; i < 10; i++ {
		id := ctx.TestDBName()
		doc := struct {
			ID string `json:"id"`
		}{
			ID: id,
		}
		if _, err := db.Put(context.Background(), doc.ID, doc); err != nil {
			ctx.Fatalf("Failed to create doc: %s", err)
		}
	}

	ctx.Run("group", func(ctx *kt.Context) {
		ctx.Run("ValidReplication", func(ctx *kt.Context) {
			ctx.Parallel()
			var success bool
			tries := 3
			for i := 0; i < tries; i++ {
				if !testValidReplication(ctx, client, dbsource, dbtarget) {
					success = true
					break
				}
				fmt.Printf("Retrying replication test after timeout")
			}
			if !success {
				ctx.Fatalf("Replication timed out %d times", tries)
			}
		})
		/* FIXME: This needs replications to be delayed. See #113
		ctx.Run("Cancel", func(ctx *kt.Context) {
			ctx.Parallel()
			dbnameA := prefix + ctx.TestDB()
			dbnameB := prefix + ctx.TestDB()
			defer ctx.Admin.DestroyDB(context.Background(), dbnameA, ctx.Options("db"))
			defer ctx.Admin.DestroyDB(context.Background(), dbnameB, ctx.Options("db"))
			replID := ctx.TestDBName()
			rep, err := callReplicate(ctx, client, dbnameA, dbnameB, replID, kivik.Options{"continuous": true})
			if !ctx.IsExpectedSuccess(err) {
				return
			}
			defer rep.Delete(context.Background())
			timeout := time.Duration(ctx.MustInt("timeoutSeconds")) * time.Second
			cx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			for i := 0; i < 2; i++ { // Try up to twice
				if err = rep.Delete(context.Background()); kivik.StatusCode(err) == kivik.StatusConflict {
					continue
				}
			}
			ctx.CheckError(err)
			for rep.IsActive() {
				if rep.State() == kivik.ReplicationStarted {
					return
				}
				select {
				case <-cx.Done():
					break
				default:
				}
				if err := rep.Update(cx); err != nil {
					if kivik.StatusCode(err) == kivik.StatusNotFound {
						// NotFound expected after the replication is cancelled
						break
					}
					ctx.Fatalf("Failed to read update: %s", err)
					break
				}
			}
			if err := cx.Err(); err != nil {
				ctx.Fatalf("context was cancelled: %s", err)
			}
			if err := rep.Err(); err != nil {
				ctx.Fatalf("Replication cancellation failed: %s", err)
			}
		})
		*/
		ctx.Run("MissingSource", func(ctx *kt.Context) {
			ctx.Parallel()
			replID := ctx.TestDBName()
			rep, err := callReplicate(ctx, client, dbtarget, "http://localhost:5984/foo", replID, nil)
			if !ctx.IsExpectedSuccess(err) {
				return
			}
			rep.Delete(context.Background())
		})
		ctx.Run("MissingTarget", func(ctx *kt.Context) {
			ctx.Parallel()
			replID := ctx.TestDBName()
			rep, err := callReplicate(ctx, client, "http://localhost:5984/foo", dbsource, replID, nil)
			if !ctx.IsExpectedSuccess(err) {
				return
			}
			rep.Delete(context.Background())
		})
	})
}

func testValidReplication(ctx *kt.Context, client *kivik.Client, dbsource, dbtarget string) (retry bool) {
	replID := ctx.TestDBName()
	rep, err := callReplicate(ctx, client, dbtarget, dbsource, replID, nil)
	if !ctx.IsExpectedSuccess(err) {
		return
	}
	// defer rep.Delete(context.Background())
	timeout := time.Duration(ctx.MustInt("timeoutSeconds")) * time.Second
	cx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var updateErr error
	for rep.IsActive() {
		select {
		case <-cx.Done():
			return
		default:
		}
		if updateErr = rep.Update(cx); updateErr != nil {
			break
		}
	}
	if updateErr != nil {
		ctx.Fatalf("Replication update failed: %s", updateErr)
	}
	ctx.Run("ReplicationResults", func(ctx *kt.Context) {
		err := rep.Err()
		if kivik.StatusCode(err) == kivik.StatusRequestTimeout {
			retry = true
			return
		}
		if !ctx.IsExpectedSuccess(err) {
			return
		}
		switch ctx.String("mode") {
		case "pouchdb":
			if rep.ReplicationID() != "" {
				ctx.Errorf("Did not expect replication ID")
			}
		default:
			if rep.ReplicationID() == "" {
				ctx.Errorf("Expected a replication ID")
			}
		}
		if rep.Source != dbsource {
			ctx.Errorf("Unexpected source. Expected: %s, Actual: %s\n", dbsource, rep.Source)
		}
		if rep.Target != dbtarget {
			ctx.Errorf("Unexpected target. Expected: %s, Actual: %s\n", dbtarget, rep.Target)
		}
		if rep.State() != kivik.ReplicationComplete {
			ctx.Errorf("Replication failed to complete. Final state: %s\n", rep.State())
		}
		if (rep.Progress() - float64(100)) > 0.0001 {
			ctx.Errorf("Expected 100%% completion, got %%%02.2f", rep.Progress())
		}
	})
	return retry
}
