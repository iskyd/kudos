package api

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/iskyd/kudos/internal/testutil"
)

var pg *testutil.Postgres

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if pg, err = testutil.StartPostgres(ctx); err != nil {
		log.Fatal(err)
	}
	code := m.Run()
	if err := pg.Terminate(ctx); err != nil {
		log.Print(err)
	}
	os.Exit(code)
}
