package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
)

// migrationLockID est la clé de l'advisory lock qui sérialise les migrations
// quand plusieurs instances démarrent en même temps.
const migrationLockID = 727_274_001

type migration struct {
	version int64
	name    string
	sql     string
}

// loadMigrations lit les fichiers {version}_{titre}.up.sql du système de fichiers.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		base, ok := strings.CutSuffix(name, ".up.sql")
		if !ok {
			continue
		}
		verStr, title, _ := strings.Cut(base, "_")
		ver, err := strconv.ParseInt(verStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration %q : numéro de version invalide", name)
		}
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: ver, name: title, sql: string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("version de migration en double : %d", out[i].version)
		}
	}
	return out, nil
}

// Migrate applique, dans l'ordre, les migrations « up » pas encore appliquées.
// Chaque migration s'exécute dans sa propre transaction : en cas d'erreur,
// elle est annulée en entier et les suivantes ne sont pas tentées.
func (s *Store) Migrate(ctx context.Context, fsys fs.FS, log *slog.Logger) error {
	migs, err := loadMigrations(fsys)
	if err != nil {
		return err
	}
	if err := s.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("table app_migrations : %w", err)
	}

	for _, m := range migs {
		if err := s.applyOne(ctx, m, log); err != nil {
			return fmt.Errorf("migration %04d_%s : %w", m.version, m.name, err)
		}
	}
	return nil
}

// ensureMigrationsTable crée la table de suivi sous le même verrou que les
// migrations : deux instances qui démarrent ensemble ne se concurrencent pas
// sur le CREATE TABLE IF NOT EXISTS.
func (s *Store) ensureMigrationsTable(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // sans effet après Commit

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS app_migrations (
		version    BIGINT      PRIMARY KEY,
		name       TEXT        NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) applyOne(ctx context.Context, m migration, log *slog.Logger) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // sans effet après Commit

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return err
	}
	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_migrations WHERE version = $1)`, m.version).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	// Sans argument, pgx utilise le protocole simple : plusieurs instructions par fichier sont permises.
	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app_migrations (version, name) VALUES ($1, $2)`, m.version, m.name); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	log.Info("migration appliquée", "version", m.version, "name", m.name)
	return nil
}
