package db

import "database/sql"

type ConfigQ struct{ db *sql.DB }

func (q *ConfigQ) Get(key string) (string, error) {
	var val string
	err := q.db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

func (q *ConfigQ) Set(key, value string) error {
	_, err := q.db.Exec(`
		INSERT INTO config(key, value, updated_at) VALUES(?, ?, unixepoch())
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = unixepoch()`,
		key, value)
	return err
}

func (q *ConfigQ) All() (map[string]string, error) {
	rows, err := q.db.Query(`SELECT key, value FROM config ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}
