package data

// open.go — convenience constructor that opens a [SQLiteStore] by path.

// OpenStore opens a SQLite database at the given path.
func OpenStore(path string) (DataStore, error) {
	return OpenSQLiteStore(path)
}
