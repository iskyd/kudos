// Package jobs holds the River client, workers and schedules.
//
// Enqueue jobs in the same transaction as the writes they depend on, so a
// rollback discards both:
//
//	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
//		// ... writes through store queries bound to tx ...
//		_, err := client.InsertTx(ctx, tx, SomeArgs{ID: id}, nil)
//		return err
//	})
//
// Job args carry IDs only, never large payloads, and workers are idempotent.
package jobs
