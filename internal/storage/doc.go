// Package storage is the site's database (SQLite, or PostgreSQL when DATABASE_URL is set): the schema and its migrations, playthroughs,
// campaign runs and their warchests, named pilots, battles, mission outcomes and the keyword ledger, checkpoints, the hangar and battle caches,
// and the MekBay links. Queries are written once with ? placeholders and rebound for Postgres.
package storage
