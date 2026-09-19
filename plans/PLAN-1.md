# Plan: Koneksi Database Supabase (stdlib Go)

## Context

- App jalan dengan stdlib saja. HTTP tetap stdlib, tanpa framework.
- Env final: satu baris `DB_URL=`. Berisi full Postgres URL direct Supabase.
- Contoh isi: `postgres://postgres:PASSWORD@db.xxx.supabase.co:5432/postgres?sslmode=require`.
- Driver: `github.com/jackc/pgx/v5/stdlib`. Ini best practice Go untuk Postgres.
- Alasan pilih pgx: rawat aktif, dukung `database/sql`, konteks dan pool benar.
- Aturan DB mati: fail-fast. App mati saat start jika DB tidak jawab.
- Alasan fail-fast: hindari mode setengah jalan. Memory store hanya untuk uji.

## Approach

- Baca `DB_URL` sekali di `config.Load`. Tanpa parsing manual.
- `db.Open(ctx, dsn)` buka pool, atur limit, ping sekali.
- `*sql.DB` teruskan ke `app.New`. Cocok ke `db.Pinger` yang sudah ada.
- `health.Service.Ready` tidak berubah bentuk. Tetap ping `Pinger`.
- `PostgresStore.List` pakai `database/sql` dengan `QueryContext`.
- `main` tutup pool dengan `defer db.Close()`.

## Files to modify

- `internal/platform/config/config.go` - tambah field `DBURL`.
- `internal/platform/db/db.go` - tambah `Open` dan pool setting.
- `cmd/api/main.go` - buka DB, fail-fast bila gagal, teruskan ke `app.New`.
- `internal/app/app.go` - terima `*sql.DB`, pakai `PostgresStore`.
- `internal/user/store_postgres.go` - implementasi `List` dengan SQL.
- `go.mod` + `go.sum` - tambah `github.com/jackc/pgx/v5`.
- `.env-example` - tetap `DB_URL=`, tambah contoh komen.

## Reuse

- `internal/platform/db/db.go` - interface `Pinger` sudah ada.
- `internal/health/service.go` - `Ready()` sudah ping `Pinger`.
- `internal/user/store_postgres.go` - konstruktor sudah ada.
- `internal/platform/config/config.go` - pola `Load()` sudah ada.

## Steps

- [x] Tambah `DBURL` ke `Config` dari env `DB_URL`.
- [x] Tambah `db.Open(ctx, dsn)` dengan ping dan pool limit.
- [x] Wire pool di `main` dengan fail-fast bila `Open` gagal.
- [x] Pilih `PostgresStore` di `app.New` bila pool ada.
- [x] Tulis `PostgresStore.List` dengan `SELECT id, name FROM users`.
- [x] Uji: `go vet`, `go test`, `curl /readyz` saat DB up dan DB down.

## Verification

- `go vet ./...` bersih.
- `go test ./...` lulus.
- Tanpa `DB_URL`: app tolak start dengan pesan jelas.
- `DB_URL` salah: app mati cepat, log sebut alasan.
- `DB_URL` benar: `/readyz` balas 200, `checks.db` = ok.
- Matikan DB: `/readyz` balas 503.
