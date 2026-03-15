package resume

import (
    "database/sql"
    _ "modernc.org/sqlite"
)

type DB struct {
    db *sql.DB
}

func Open(path string) *DB {

    db, err := sql.Open("sqlite", path)
    if err != nil {
        panic(err)
    }

    return &DB{db}
}
