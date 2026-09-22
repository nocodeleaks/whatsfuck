package sqlstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/nocodeleaks/whatsfuck/store"
	"github.com/nocodeleaks/whatsfuck/types"
)

type messageSecretsConnector struct {
	pnMigrationTestConnector
	args [][]driver.NamedValue
}

func (c *messageSecretsConnector) Connect(context.Context) (driver.Conn, error) {
	return &messageSecretsConn{pnMigrationTestConn: pnMigrationTestConn{state: c.state}, owner: c}, nil
}

type messageSecretsConn struct {
	pnMigrationTestConn
	owner *messageSecretsConnector
}

func (c *messageSecretsConn) ExecContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Result, error) {
	c.owner.args = append(c.owner.args, append([]driver.NamedValue(nil), args...))
	return driver.RowsAffected(1), nil
}

func TestMessageSecretBatchFiltersIncompleteEntries(t *testing.T) {
	connector := &messageSecretsConnector{pnMigrationTestConnector: pnMigrationTestConnector{state: &pnMigrationTestDB{}}}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })
	s := NewSQLStore(NewWithDB(db, "postgres", nil), types.NewJID("1234", types.HiddenUserServer))
	valid := store.MessageSecretInsert{Chat: types.NewJID("2222", types.BotServer), Sender: types.NewJID("1234", types.HiddenUserServer), ID: "secret", Secret: []byte("value")}
	missingChat, missingSender, missingID, missingSecret := valid, valid, valid, valid
	missingChat.Chat = types.EmptyJID
	missingSender.Sender = types.EmptyJID
	missingID.ID = ""
	missingSecret.Secret = nil
	invalid := []store.MessageSecretInsert{missingChat, missingSender, missingID, missingSecret}
	if err := s.PutMessageSecrets(context.Background(), invalid); err != nil {
		t.Fatal(err)
	}
	if len(connector.args) != 0 || connector.state.begins != 0 {
		t.Fatal("invalid entries touched the database")
	}
	entries := append(append([]store.MessageSecretInsert(nil), invalid...), valid)
	before := append([]store.MessageSecretInsert(nil), entries...)
	if err := s.PutMessageSecrets(context.Background(), entries); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entries, before) {
		t.Fatal("batch filtering changed the caller's slice")
	}
	if len(connector.args) != 1 || len(connector.args[0]) != 5 {
		t.Fatalf("incorrect batch parameters: %#v", connector.args)
	}
	if got := connector.args[0][3].Value; got != "secret" {
		t.Fatalf("stored ID = %v", got)
	}
}
