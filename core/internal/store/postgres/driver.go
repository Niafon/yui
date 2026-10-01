// Legacy PostgreSQL driver retained for v0.2 data export and compatibility.
// SQLite is the default production backend since ADR-037.
package postgres

import _ "github.com/jackc/pgx/v5/stdlib"
