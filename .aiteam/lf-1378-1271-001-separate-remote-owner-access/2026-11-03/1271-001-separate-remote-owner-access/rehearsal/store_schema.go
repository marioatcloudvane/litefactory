// Temporary store-owned inspector used only by the authorization rehearsal builds.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
)

func RehearsalSchemaFingerprint(path string) (string, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return "", err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT type,name,coalesce(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var kind, name, ddl string
		if err := rows.Scan(&kind, &name, &ddl); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s\x00%s\x00%s\n", kind, name, ddl)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	v := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(v[:]), nil
}
